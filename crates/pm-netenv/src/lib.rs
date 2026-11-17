//! Port of the Go `service/netenv` package.
//!
//! The crate carries the network environment that other modules consume: the
//! online status with its change events, the captive portal, the interface
//! cache, the local address book and the physical default interfaces.
//!
//! # Compatibility notes
//!
//! * The OS access is injected through [`InterfaceSource`]; [`SystemInterfaces`]
//!   is the Linux implementation. Physical default interface detection on
//!   other platforms reports the Go "not supported" error.
//! * DNS/HTTP probing lives in [`online_check`]; the ICMP listener and probe
//!   support live in [`icmp`]. Both are injected so no root is needed in tests.
//! * Go keeps the caches in package-level sync primitives; here they live in
//!   [`InterfaceCache`], [`AddressCache`] and [`EnvironmentCache`], with
//!   [`network`] and [`environment`] providing process-wide defaults.
//! * Nameservers, gateways and the NetworkManager connectivity state are read
//!   from files and the `busctl` command line tool; see [`dbus`] for the
//!   deviation from Go's native D-Bus client.
//! * Notifications are emitted as [`NotificationEvent`]s through an injected
//!   [`NotificationSink`]. The caller (which owns the notification manager)
//!   sends them; see [`notifications`].
//!
//! # Platform support
//!
//! The crate compiles on Windows. [`SystemEnvironment`] reads nameservers and
//! gateways from PowerShell (`Get-NetIPConfiguration` / `Get-DnsClient`), the
//! same commands Go uses. The remaining OS-facing sources are Unix-only:
//! [`SystemInterfaces`] reports no interfaces because Go's
//! `GetAdaptersAddresses` enumeration needs Windows-only unsafe FFI, and
//! [`SystemIcmpSocket`] cannot be opened because Go's `SIO_RCVALL` raw socket
//! setup likewise needs unsafe FFI. The injected sources and all
//! parsing/status logic work with test data on every platform.

use std::net::IpAddr;
use std::sync::atomic::{AtomicBool, AtomicU8, Ordering};
use std::sync::{Arc, OnceLock, RwLock};

use pm_mgr::EventMgr;
use serde::{Deserialize, Serialize};

mod addresses;
pub mod dbus;
pub mod environment;
mod flag;
mod icmp;
mod interfaces;
mod network_change;
mod notifications;
mod online_check;
mod system;
mod windows_env;

pub use addresses::{AddressCache, LocalNetwork};
pub use dbus::{
    connectivity_state_from_dbus, nameservers_from_dbus, ConnectivityState, DbusClient, DbusValue,
    SystemDbus,
};
pub use environment::{
    add_nameservers, connectivity_state, environment, gateways, gateways_info, nameservers,
    parse_ipv4_routes, parse_ipv6_routes, parse_resolvconf, EnvironmentCache, EnvironmentSource,
    GatewayInfo, Nameserver, SystemEnvironment, IPV4_ROUTE_PATH, IPV6_ROUTE_PATH, RESOLV_CONF_PATH,
};
pub use flag::{
    global_source, network_changed_flag, notify_network_change, FlagPair, NetworkChangeFlag,
    NetworkChangeSource,
};
pub use icmp::{
    build_echo, embedded_echo_ids, interface_index, parse_icmp_v4, recv_icmp, recv_timeout,
    run_probe, target_socket_address, IcmpListener, IcmpListenerGuard, IcmpPacket, IcmpReply,
    IcmpSocket, IcmpV4Packet, IcmpV4Type, PacketReceiver, ProbeOutcome, RecvOutcome,
    SystemIcmpSocket, ICMP_CHANNEL_CAPACITY, ICMP_INITIAL_MAX_HOPS, ICMP_PROBE_ATTEMPTS_PER_HOP,
    ICMP_SEND_RETRIES, ICMP_SEND_RETRY_DELAY,
};
pub use interfaces::{
    canonical_mac, is_routable_unicast, network_contains, normalize_ip, InterfaceCache,
    InterfaceInfo, InterfaceSource, NetInterface, PhysicalDefaultInterfaces, RawInterface,
    RouteCandidate,
};
pub use network_change::{NetworkChangeMonitor, OFFLINE_CHECK_INTERVAL, ONLINE_CHECK_INTERVAL};
pub use notifications::{
    captive_portal_event, notification_change, online_status_event, NoopNotificationSink,
    NotificationAction, NotificationChange, NotificationEvent, NotificationSink,
    CAPTIVE_PORTAL_EVENT_ID, CAPTIVE_PORTAL_MESSAGE, CAPTIVE_PORTAL_TITLE, IGNORE_ACTION_ID,
    IGNORE_ACTION_TEXT, LIMITED_EVENT_ID, LIMITED_MESSAGE, LIMITED_TITLE, NOTIFICATION_TYPE_INFO,
    OFFLINE_EVENT_ID, OFFLINE_MESSAGE, OFFLINE_TITLE, OPEN_PORTAL_ACTION_TEXT,
    OPEN_PORTAL_ACTION_TYPE,
};
pub use online_check::{
    check_online_status, dynamic_check_delay, is_connectivity_domain, no_portal_probe,
    shared_probe, DnsCheck, DnsCheckAlreadySet, DnsCheckOutcome, OnlineCheckEnv, OnlineCheckResult,
    OnlineMonitor, PortalProbe, PortalProbeResult, StaticDnsCheck, StaticPortalProbe,
    SystemPortalProbe,
    CONNECTIVITY_DOMAINS, DNS_TEST_DOMAIN, DNS_TEST_EXPECTED_IP, IGNORE_IPS_IN_ONLINE_STATUS_CHECK,
    PORTAL_TEST_IP, PORTAL_TEST_URL,
};
pub use system::SystemInterfaces;

/// The process-wide network caches.
pub struct Network {
    /// Interface cache backed by the system interfaces.
    pub interfaces: Arc<InterfaceCache>,
    /// Local address cache backed by the system interfaces.
    pub addresses: Arc<AddressCache>,
    /// Nameserver and gateway cache backed by files and D-Bus.
    pub environment: &'static EnvironmentCache,
}

static NETWORK: OnceLock<Network> = OnceLock::new();

/// Returns the process-wide network caches.
pub fn network() -> &'static Network {
    NETWORK.get_or_init(|| {
        let source: Arc<dyn InterfaceSource> = Arc::new(SystemInterfaces);
        Network {
            interfaces: Arc::new(InterfaceCache::new(
                Arc::clone(&source),
                network_changed_flag(),
            )),
            addresses: Arc::new(AddressCache::new(source, network_changed_flag())),
            environment: environment(),
        }
    })
}

/// Domain name used to point to a detected captive portal IP.
pub const SPECIAL_CAPTIVE_PORTAL_DOMAIN: &str = "captiveportal.invalid.";

/// Online status values, like Go `netenv.OnlineStatus`.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, PartialOrd, Ord)]
#[repr(u8)]
pub enum OnlineStatus {
    /// The status was not determined yet.
    #[default]
    Unknown = 0,
    Offline = 1,
    /// Local network only.
    Limited = 2,
    /// There seems to be an internet connection, but we are being intercepted,
    /// possibly by a captive portal.
    Portal = 3,
    /// We seem to be online, but without full connectivity.
    SemiOnline = 4,
    Online = 5,
}

impl OnlineStatus {
    /// Creates a status from its numeric value; unknown values map to
    /// [`OnlineStatus::Unknown`], like Go's `String` fallback.
    pub fn from_u8(value: u8) -> OnlineStatus {
        match value {
            1 => OnlineStatus::Offline,
            2 => OnlineStatus::Limited,
            3 => OnlineStatus::Portal,
            4 => OnlineStatus::SemiOnline,
            5 => OnlineStatus::Online,
            _ => OnlineStatus::Unknown,
        }
    }

    /// Returns whether the status is online or semi-online, like Go `Online`.
    pub fn is_online(self) -> bool {
        matches!(self, OnlineStatus::Online | OnlineStatus::SemiOnline)
    }

    /// Returns the name of the status, byte-equal to Go `OnlineStatus.String`.
    pub fn as_str(self) -> &'static str {
        match self {
            OnlineStatus::Offline => "Offline",
            OnlineStatus::Limited => "Limited",
            OnlineStatus::Portal => "Portal",
            OnlineStatus::SemiOnline => "SemiOnline",
            OnlineStatus::Online => "Online",
            OnlineStatus::Unknown => "Unknown",
        }
    }
}

impl Serialize for OnlineStatus {
    /// Serializes as the numeric value, like Go's `uint8` base type.
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_u8(*self as u8)
    }
}

impl<'de> Deserialize<'de> for OnlineStatus {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        Ok(OnlineStatus::from_u8(u8::deserialize(deserializer)?))
    }
}

impl std::fmt::Display for OnlineStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Information about a detected captive portal.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct CaptivePortal {
    #[serde(rename = "URL")]
    pub url: String,
    #[serde(rename = "Domain")]
    pub domain: String,
    /// IP of the portal; serialized as a string like Go's `net.IP`.
    #[serde(rename = "IP")]
    pub ip: Option<IpAddr>,
}

impl CaptivePortal {
    /// Creates a captive portal description.
    pub fn new(url: &str, domain: &str, ip: Option<IpAddr>) -> CaptivePortal {
        CaptivePortal {
            url: url.to_string(),
            domain: domain.to_string(),
            ip,
        }
    }

    /// Derives the portal domain and IP from its URL, like Go's captive portal
    /// handling: IP literals use the special portal domain.
    pub fn from_url(url: &str) -> CaptivePortal {
        let host = url
            .split_once("://")
            .map(|(_, rest)| rest)
            .unwrap_or(url)
            .split('/')
            .next()
            .unwrap_or_default();
        let host = host.rsplit_once('@').map_or(host, |(_, host)| host);
        // Strip a port from host:port, keeping IPv6 literals intact.
        let host = match host.strip_prefix('[') {
            Some(rest) => rest.split(']').next().unwrap_or(rest),
            None => host.split(':').next().unwrap_or(host),
        };

        match host.parse::<IpAddr>() {
            Ok(ip) => CaptivePortal {
                url: url.to_string(),
                domain: SPECIAL_CAPTIVE_PORTAL_DOMAIN.to_string(),
                ip: Some(ip),
            },
            Err(_) => CaptivePortal {
                url: url.to_string(),
                domain: host.to_string(),
                ip: None,
            },
        }
    }

    /// Returns whether the other portal is the same, like Go `CaptivePortal.Equal`.
    pub fn equal(&self, other: &CaptivePortal) -> bool {
        self.url == other.url && self.ip == other.ip && self.domain == other.domain
    }
}

/// The network environment state.
pub struct NetEnv {
    online_status: AtomicU8,
    captive_portal: RwLock<Option<CaptivePortal>>,
    online_status_events: EventMgr<OnlineStatus>,
    notification_sink: RwLock<Arc<dyn NotificationSink>>,
    status_notification_id: RwLock<Option<String>>,
}

impl Default for NetEnv {
    fn default() -> Self {
        NetEnv::new()
    }
}

impl NetEnv {
    /// Creates an empty network environment without a notification owner.
    pub fn new() -> NetEnv {
        NetEnv::with_notification_sink(Arc::new(NoopNotificationSink))
    }

    /// Creates a network environment with an injected notification sink.
    pub fn with_notification_sink(sink: Arc<dyn NotificationSink>) -> NetEnv {
        NetEnv {
            online_status: AtomicU8::new(OnlineStatus::Unknown as u8),
            captive_portal: RwLock::new(None),
            online_status_events: EventMgr::new("online status change"),
            notification_sink: RwLock::new(sink),
            status_notification_id: RwLock::new(None),
        }
    }

    /// Replaces the notification sink, e.g. once the core is initialized.
    pub fn set_notification_sink(&self, sink: Arc<dyn NotificationSink>) {
        *self.notification_sink.write().expect("lock poisoned") = sink;
    }

    /// Returns the current online status.
    pub fn online_status(&self) -> OnlineStatus {
        OnlineStatus::from_u8(self.online_status.load(Ordering::SeqCst))
    }

    /// Applies the result of an online status check, updating the status and
    /// the captive portal.
    pub fn apply_online_check(&self, result: &OnlineCheckResult) -> bool {
        let portal = result
            .portal_url
            .as_ref()
            .map(|url| CaptivePortal::from_url(url));
        self.update_online_status(result.status, portal)
    }

    /// Returns whether the status is online or semi-online.
    pub fn online(&self) -> bool {
        self.online_status().is_online()
    }

    /// Returns the current captive portal, if any.
    pub fn captive_portal(&self) -> Option<CaptivePortal> {
        self.captive_portal.read().expect("lock poisoned").clone()
    }

    /// Returns the event manager for online status changes.
    pub fn online_status_events(&self) -> &EventMgr<OnlineStatus> {
        &self.online_status_events
    }

    /// Updates the online status and the captive portal, like Go
    /// `updateOnlineStatus`.
    ///
    /// Returns whether the status changed and submits an event when it did.
    pub fn update_online_status(
        &self,
        status: OnlineStatus,
        portal: Option<CaptivePortal>,
    ) -> bool {
        // Go sets the captive portal only once per detection and clears it when
        // the status update no longer carries a portal URL.
        let previous_portal = self.captive_portal();
        let had_portal = previous_portal
            .as_ref()
            .is_some_and(|portal| !portal.url.is_empty());
        let portal = match (previous_portal, portal) {
            (Some(previous), Some(next)) if !previous.url.is_empty() && !next.url.is_empty() => {
                Some(previous)
            }
            (_, next) => next,
        };
        *self.captive_portal.write().expect("lock poisoned") = portal.clone();

        let previous = self.online_status.swap(status as u8, Ordering::SeqCst);
        let changed = previous != status as u8;
        // A portal can disappear while the status number stays the same; its
        // notification still has to be deleted in that case (Go separates the
        // captive portal notification from the online status notification).
        let portal_removed = had_portal && portal.as_ref().map_or(true, |p| p.url.is_empty());
        if !changed && !portal_removed {
            return false;
        }

        if changed {
            tracing::info!(status = %status, "netenv: online status changed");
            self.online_status_events.submit(status);
        }

        let mut status_id = self.status_notification_id.write().expect("lock poisoned");
        let change = notification_change(status, portal.as_ref(), had_portal, status_id.as_deref());
        let new_id = change
            .notify
            .iter()
            .find(|event| event.event_id == OFFLINE_EVENT_ID || event.event_id == LIMITED_EVENT_ID)
            .map(|event| event.event_id.clone());
        if new_id.is_some() || change.delete_online_status {
            *status_id = new_id;
        }
        drop(status_id);
        self.notification_sink
            .read()
            .expect("lock poisoned")
            .handle(change);
        changed
    }
}

/// Returns the process-wide network environment.
pub fn global() -> &'static NetEnv {
    static NETENV: OnceLock<NetEnv> = OnceLock::new();
    NETENV.get_or_init(NetEnv::new)
}

/// Returns the current online status, like Go `netenv.GetOnlineStatus`.
pub fn get_online_status() -> OnlineStatus {
    global().online_status()
}

/// Returns whether the system is online, like Go `netenv.Online`.
pub fn online() -> bool {
    global().online()
}

/// Returns the current captive portal, like Go `netenv.GetCaptivePortal`.
pub fn get_captive_portal() -> Option<CaptivePortal> {
    global().captive_portal()
}

/// Returns a shared handle to the process-wide network environment.
pub fn shared() -> Arc<NetEnv> {
    static SHARED: OnceLock<Arc<NetEnv>> = OnceLock::new();
    Arc::clone(SHARED.get_or_init(|| Arc::new(NetEnv::new())))
}

static CONNECTED_TO_SPN: AtomicBool = AtomicBool::new(false);
static CONNECTED_TO_DNS: AtomicBool = AtomicBool::new(false);

/// Records whether the core is connected to the SPN, like Go
/// `netenv.ConnectedToSPN`.
pub fn set_connected_to_spn(connected: bool) {
    CONNECTED_TO_SPN.store(connected, Ordering::SeqCst);
}

/// Returns whether the core is connected to the SPN, like Go
/// `netenv.ConnectedToSPN.IsSet`.
pub fn is_connected_to_spn() -> bool {
    CONNECTED_TO_SPN.load(Ordering::SeqCst)
}

/// Records whether the core is connected to the internal DNS, like Go
/// `netenv.ConnectedToDNS`.
pub fn set_connected_to_dns(connected: bool) {
    CONNECTED_TO_DNS.store(connected, Ordering::SeqCst);
}

/// Returns whether the core is connected to the internal DNS, like Go
/// `netenv.ConnectedToDNS.IsSet`.
pub fn is_connected_to_dns() -> bool {
    CONNECTED_TO_DNS.load(Ordering::SeqCst)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_names_match_go() {
        let cases = [
            (0, "Unknown"),
            (1, "Offline"),
            (2, "Limited"),
            (3, "Portal"),
            (4, "SemiOnline"),
            (5, "Online"),
            (9, "Unknown"),
        ];
        for (value, name) in cases {
            assert_eq!(OnlineStatus::from_u8(value).as_str(), name);
        }
        assert!(OnlineStatus::Online.is_online());
        assert!(OnlineStatus::SemiOnline.is_online());
        assert!(!OnlineStatus::Portal.is_online());
    }

    #[test]
    fn captive_portal_is_derived_from_the_url() {
        let portal = CaptivePortal::from_url("http://192.0.2.10/login");
        assert_eq!(portal.domain, SPECIAL_CAPTIVE_PORTAL_DOMAIN);
        assert_eq!(portal.ip, Some("192.0.2.10".parse().unwrap()));

        let portal = CaptivePortal::from_url("https://portal.example.com:8443/login");
        assert_eq!(portal.domain, "portal.example.com");
        assert_eq!(portal.ip, None);

        let portal = CaptivePortal::from_url("http://[2001:db8::1]:8080/");
        assert_eq!(portal.domain, SPECIAL_CAPTIVE_PORTAL_DOMAIN);
        assert_eq!(portal.ip, Some("2001:db8::1".parse().unwrap()));

        assert!(CaptivePortal::from_url("http://a.example/")
            .equal(&CaptivePortal::from_url("http://a.example/")));
    }

    #[test]
    fn updates_emit_events_only_on_change() {
        let netenv = NetEnv::new();
        let mut events = netenv.online_status_events().subscribe("test", 8);

        assert!(!netenv.online(), "unknown is not online");
        assert!(!netenv.update_online_status(OnlineStatus::Unknown, None));
        assert!(events.try_recv().is_err(), "no event without a change");

        assert!(netenv.update_online_status(
            OnlineStatus::Online,
            Some(CaptivePortal::from_url("http://portal/login")),
        ));
        assert!(netenv.online());
        assert_eq!(events.try_recv().unwrap(), OnlineStatus::Online);
        assert!(netenv.captive_portal().is_some());

        // Going back offline clears the portal.
        assert!(netenv.update_online_status(OnlineStatus::Offline, None));
        assert_eq!(events.try_recv().unwrap(), OnlineStatus::Offline);
        assert!(netenv.captive_portal().is_none());
    }
}
