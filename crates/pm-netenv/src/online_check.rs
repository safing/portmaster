//! Online status checks, port of Go `service/netenv/online-status.go`.
//!
//! The probe itself is a chain with early exits: SPN/DNS connectivity, assigned
//! addresses, an HTTP request to a documentation address, and a DNS check. The
//! network operations are injected ([`PortalProbe`], [`DnsCheck`]) so the logic
//! can be tested; [`SystemPortalProbe`] is the real HTTP probe.

use std::net::{IpAddr, Ipv4Addr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use pm_netstate::{ip_scope, IpScope};
use tokio_util::sync::CancellationToken;

use crate::{AddressCache, NetEnv, OnlineStatus};

/// Portal test IP (documentation range, like Go).
pub const PORTAL_TEST_IP: Ipv4Addr = Ipv4Addr::new(192, 0, 2, 1);
/// Portal test URL.
pub const PORTAL_TEST_URL: &str = "http://192.0.2.1/";

/// Special IP used by the Android VPN service that must be ignored.
pub const IGNORE_IPS_IN_ONLINE_STATUS_CHECK: &[Ipv4Addr] = &[Ipv4Addr::new(100, 127, 247, 245)];

/// Domain used for the DNS check.
pub const DNS_TEST_DOMAIN: &str = "online-check.safing.io.";
/// Expected answer of the DNS check (`\0ACK`).
pub const DNS_TEST_EXPECTED_IP: Ipv4Addr = Ipv4Addr::new(0, 65, 67, 75);

/// Domains used by operating systems for connectivity checks.
pub const CONNECTIVITY_DOMAINS: &[&str] = &[
    // The special captive portal domain is checked separately.
    // Windows.
    "dns.msftncsi.com.",
    "msftncsi.com.",
    "www.msftncsi.com.",
    "microsoftconnecttest.com.",
    "www.microsoftconnecttest.com.",
    "ipv6.microsoftconnecttest.com.",
    // Apple.
    "captive.apple.com.",
    // Linux distributions.
    "connectivity-check.ubuntu.com.",
    "nmcheck.gnome.org.",
    "network-test.debian.org.",
    "204.pop-os.org.",
    "conncheck.opensuse.org.",
    "ping.archlinux.org",
    // Android.
    "connectivitycheck.gstatic.com.",
    // Other.
    "neverssl.com.",
    "detectportal.firefox.com.",
];

/// Returns whether the domain is used for connectivity checks.
///
/// `portal_domain` is the domain of the currently detected captive portal.
pub fn is_connectivity_domain(domain: &str, portal_domain: &str) -> bool {
    if domain.is_empty() {
        return false;
    }
    CONNECTIVITY_DOMAINS.contains(&domain)
        || (!portal_domain.is_empty() && domain == portal_domain)
}

/// Result of the HTTP portal probe.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PortalProbeResult {
    /// The request was intercepted with a redirect to this URL.
    Redirect(String),
    /// The request succeeded with status 200: we are behind a portal.
    Ok,
    /// Any other response code.
    Other(u16),
    /// The request failed (the expected outcome without a portal).
    Error(String),
}

/// Performs the HTTP portal probe.
pub trait PortalProbe: Send + Sync {
    /// Requests [`PORTAL_TEST_URL`].
    fn probe(&self) -> PortalProbeResult;
}

/// Result of the DNS check.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DnsCheckOutcome {
    /// The resolver answered with these addresses.
    Answered(Vec<IpAddr>),
    /// The resolver answered, but reported an acceptable error.
    AnsweredWithError(String),
    /// The query failed.
    Failed(String),
}

/// Checks the DNS test domain.
pub trait DnsCheck: Send + Sync {
    /// Resolves [`DNS_TEST_DOMAIN`].
    fn query(&self, fqdn: &str) -> DnsCheckOutcome;
}

/// The result of one online status check.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OnlineCheckResult {
    /// Determined status.
    pub status: OnlineStatus,
    /// Captive portal URL, when one was detected.
    pub portal_url: Option<String>,
    /// Explanation of the result, like the Go log comment.
    pub comment: String,
}

/// Inputs of the online status check.
pub struct OnlineCheckEnv<'a> {
    /// Address cache for `GetAssignedAddresses`.
    pub addresses: &'a AddressCache,
    /// HTTP portal probe.
    pub portal: &'a dyn PortalProbe,
    /// DNS check; `None` disables it, like a nil `DNSTestQueryFunc`.
    pub dns: Option<&'a dyn DnsCheck>,
    /// Whether the core is connected to the SPN.
    pub connected_to_spn: bool,
    /// Whether the core is connected to the internal DNS.
    pub connected_to_dns: bool,
}

/// Runs the online status check chain, like Go `checkOnlineStatus`.
pub fn check_online_status(env: &OnlineCheckEnv<'_>) -> OnlineCheckResult {
    if env.connected_to_spn {
        return online(OnlineStatus::Online, "connected to SPN");
    }
    if env.connected_to_dns {
        return online(OnlineStatus::Online, "connected to DNS");
    }

    // 1) Check the assigned addresses.
    match env.addresses.get_assigned_addresses() {
        Err(error) => {
            tracing::warn!("netenv: failed to get assigned network addresses: {error}");
        }
        Ok((ipv4, ipv6)) => {
            let mut lan = false;
            for ip in ipv4 {
                if IGNORE_IPS_IN_ONLINE_STATUS_CHECK.contains(&ip) {
                    continue;
                }
                match ip_scope(IpAddr::V4(ip)) {
                    IpScope::SiteLocal => lan = true,
                    IpScope::Global => {
                        return online(
                            OnlineStatus::Online,
                            "global IPv4 interface detected",
                        )
                    }
                    _ => {}
                }
            }
            for ip in ipv6 {
                let ip = IpAddr::V6(ip);
                if IGNORE_IPS_IN_ONLINE_STATUS_CHECK
                    .iter()
                    .any(|ignored| IpAddr::V4(*ignored) == ip)
                {
                    continue;
                }
                match ip_scope(ip) {
                    // IPv6 global addresses are also used in local networks.
                    IpScope::SiteLocal | IpScope::Global => lan = true,
                    _ => {}
                }
            }
            if !lan {
                return online(
                    OnlineStatus::Offline,
                    "no local or global interfaces detected",
                );
            }
        }
    }

    // 2) Try the HTTP portal test.
    match env.portal.probe() {
        PortalProbeResult::Redirect(url) => {
            return OnlineCheckResult {
                status: OnlineStatus::Portal,
                portal_url: Some(url),
                comment: "portal test request succeeded with redirect".to_string(),
            }
        }
        PortalProbeResult::Ok => {
            return OnlineCheckResult {
                status: OnlineStatus::Portal,
                portal_url: Some(format!("http://{}/", crate::SPECIAL_CAPTIVE_PORTAL_DOMAIN)),
                comment: "portal test request succeeded".to_string(),
            }
        }
        PortalProbeResult::Other(code) => {
            tracing::debug!("netenv: unexpected http portal test response code: {code}");
        }
        PortalProbeResult::Error(error) => {
            tracing::debug!("netenv: http portal test failed: {error}");
        }
    }

    // 3) Resolve the DNS check domain.
    let Some(dns) = env.dns else {
        return online(
            OnlineStatus::Online,
            "all checks passed, dns query check disabled",
        );
    };
    match dns.query(DNS_TEST_DOMAIN) {
        DnsCheckOutcome::AnsweredWithError(error) => online(
            OnlineStatus::Online,
            &format!("all checks passed, acceptable result for dns query check: {error}"),
        ),
        DnsCheckOutcome::Answered(ips) => {
            let expected = IpAddr::V4(DNS_TEST_EXPECTED_IP);
            match ips.first() {
                Some(ip) if *ip == expected => {
                    online(OnlineStatus::Online, "all checks passed")
                }
                Some(ip) => {
                    tracing::warn!("netenv: dns query check response mismatched: got {ip}");
                    online(
                        OnlineStatus::Online,
                        "all checks passed, dns query check response mismatched",
                    )
                }
                None => {
                    tracing::warn!("netenv: dns query check response mismatched: empty response");
                    online(
                        OnlineStatus::Online,
                        "all checks passed, dns query check response was empty",
                    )
                }
            }
        }
        DnsCheckOutcome::Failed(error) => {
            tracing::warn!("netenv: dns query check failed: {error}");
            online(OnlineStatus::Offline, "dns query check failed")
        }
    }
}

fn online(status: OnlineStatus, comment: &str) -> OnlineCheckResult {
    OnlineCheckResult {
        status,
        portal_url: None,
        comment: comment.to_string(),
    }
}

/// The system HTTP probe for the portal test URL.
pub struct SystemPortalProbe {
    agent: ureq::Agent,
}

impl SystemPortalProbe {
    /// Creates the probe with Go's one-second timeout and no redirects.
    pub fn new() -> SystemPortalProbe {
        let config = ureq::Agent::config_builder()
            .max_redirects(0)
            .timeout_global(Some(Duration::from_secs(1)))
            .build();
        SystemPortalProbe {
            agent: ureq::Agent::new_with_config(config),
        }
    }
}

impl Default for SystemPortalProbe {
    fn default() -> Self {
        SystemPortalProbe::new()
    }
}

impl PortalProbe for SystemPortalProbe {
    fn probe(&self) -> PortalProbeResult {
        match self.agent.get(PORTAL_TEST_URL).call() {
            Ok(response) => {
                let status = response.status().as_u16();
                if (300..400).contains(&status) {
                    if let Some(location) = response.headers().get("location") {
                        let location = location.to_str().unwrap_or_default();
                        if !location.is_empty() {
                            return PortalProbeResult::Redirect(location.to_string());
                        }
                    }
                    return PortalProbeResult::Other(status);
                }
                if status == 200 {
                    return PortalProbeResult::Ok;
                }
                PortalProbeResult::Other(status)
            }
            Err(ureq::Error::StatusCode(code)) => PortalProbeResult::Other(code),
            Err(error) => PortalProbeResult::Error(error.to_string()),
        }
    }
}

/// A probe with a fixed result, for tests.
pub struct StaticPortalProbe {
    result: PortalProbeResult,
}

impl StaticPortalProbe {
    /// Creates a probe that always returns `result`.
    pub fn new(result: PortalProbeResult) -> StaticPortalProbe {
        StaticPortalProbe { result }
    }
}

impl PortalProbe for StaticPortalProbe {
    fn probe(&self) -> PortalProbeResult {
        self.result.clone()
    }
}

/// A DNS check with a fixed result, for tests.
pub struct StaticDnsCheck {
    outcome: DnsCheckOutcome,
}

impl StaticDnsCheck {
    /// Creates a check that always returns `outcome`.
    pub fn new(outcome: DnsCheckOutcome) -> StaticDnsCheck {
        StaticDnsCheck { outcome }
    }
}

impl DnsCheck for StaticDnsCheck {
    fn query(&self, _fqdn: &str) -> DnsCheckOutcome {
        self.outcome.clone()
    }
}

/// Convenience for tests: a probe that always fails, like no portal.
pub fn no_portal_probe() -> StaticPortalProbe {
    StaticPortalProbe::new(PortalProbeResult::Error("timeout".to_string()))
}

/// Wraps a probe in an `Arc`.
pub fn shared_probe<T: PortalProbe + 'static>(probe: T) -> Arc<dyn PortalProbe> {
    Arc::new(probe)
}

/// Interval of periodic checks per status, like Go `getDynamicStatusTrigger`.
///
/// `Online` is `None`: the status stays until a resolver reports problems.
pub fn dynamic_check_delay(status: OnlineStatus) -> Option<Duration> {
    match status {
        OnlineStatus::Offline => Some(Duration::from_secs(10)),
        OnlineStatus::Limited | OnlineStatus::Portal => Some(Duration::from_secs(5)),
        OnlineStatus::SemiOnline => Some(Duration::from_secs(60)),
        OnlineStatus::Online => None,
        OnlineStatus::Unknown => Some(Duration::from_secs(5 * 60)),
    }
}

/// Runs online status checks and applies their result, like Go
/// `monitorOnlineStatus`.
pub struct OnlineMonitor {
    netenv: Arc<NetEnv>,
    addresses: Arc<AddressCache>,
    portal: Arc<dyn PortalProbe>,
    dns: Option<Arc<dyn DnsCheck>>,
    connected_to_spn: AtomicBool,
    connected_to_dns: AtomicBool,
    trigger: tokio::sync::Notify,
}

impl OnlineMonitor {
    /// Creates a monitor. The first check runs when [`OnlineMonitor::run`]
    /// starts, like the Go worker.
    pub fn new(
        netenv: Arc<NetEnv>,
        addresses: Arc<AddressCache>,
        portal: Arc<dyn PortalProbe>,
        dns: Option<Arc<dyn DnsCheck>>,
    ) -> OnlineMonitor {
        OnlineMonitor {
            netenv,
            addresses,
            portal,
            dns,
            connected_to_spn: AtomicBool::new(false),
            connected_to_dns: AtomicBool::new(false),
            trigger: tokio::sync::Notify::new(),
        }
    }

    /// Returns the state the monitor updates.
    pub fn netenv(&self) -> &Arc<NetEnv> {
        &self.netenv
    }

    /// Updates whether the core is connected to the SPN.
    pub fn set_connected_to_spn(&self, connected: bool) {
        self.connected_to_spn.store(connected, Ordering::SeqCst);
    }

    /// Updates whether the core is connected to the internal DNS.
    pub fn set_connected_to_dns(&self, connected: bool) {
        self.connected_to_dns.store(connected, Ordering::SeqCst);
    }

    /// Triggers a check as soon as possible, like
    /// `TriggerOnlineStatusInvestigation`.
    pub fn trigger(&self) {
        self.trigger.notify_one();
    }

    /// Runs one check, applies it and returns the result.
    pub fn check_now(&self) -> OnlineCheckResult {
        let env = OnlineCheckEnv {
            addresses: &self.addresses,
            portal: self.portal.as_ref(),
            dns: self.dns.as_deref(),
            connected_to_spn: self.connected_to_spn.load(Ordering::SeqCst),
            connected_to_dns: self.connected_to_dns.load(Ordering::SeqCst),
        };
        let result = check_online_status(&env);
        self.netenv.apply_online_check(&result);
        result
    }

    /// Runs checks forever: immediately, on triggers and after the per-status
    /// delay. The task exits when the token is cancelled.
    ///
    /// The probe runs on a blocking thread, because HTTP and DNS are blocking
    /// calls.
    pub async fn run(self: &Arc<Self>, token: CancellationToken) {
        loop {
            let monitor = Arc::clone(self);
            if let Err(error) = tokio::task::spawn_blocking(move || monitor.check_now()).await {
                tracing::warn!("netenv: online status check failed: {error}");
            }

            let delay = dynamic_check_delay(self.netenv.online_status());
            tokio::select! {
                () = token.cancelled() => return,
                () = self.trigger.notified() => {}
                () = sleep_optional(delay) => {}
            }
        }
    }
}

async fn sleep_optional(delay: Option<Duration>) {
    match delay {
        Some(delay) => tokio::time::sleep(delay).await,
        None => std::future::pending::<()>().await,
    }
}
