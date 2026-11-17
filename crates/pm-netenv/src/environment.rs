//! System nameservers and gateways, port of Go `service/netenv/environment.go`
//! and `environment_linux.go`.
//!
//! Like Go, both lists are cached and only refreshed when the network change
//! flag fires. All file/D-Bus access lives behind [`EnvironmentSource`], so
//! the caching and parsing logic is testable without touching the host.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, OnceLock, RwLock};

use pm_netutils::is_valid_fqdn;
use serde::{Deserialize, Serialize};

use crate::dbus::ConnectivityState;
#[cfg(not(windows))]
use crate::dbus::{connectivity_state_from_dbus, nameservers_from_dbus, SystemDbus};
use crate::{network_changed_flag, NetworkChangeFlag};

/// Path of the resolver configuration file.
pub const RESOLV_CONF_PATH: &str = "/etc/resolv.conf";
/// Path of the IPv4 routing table.
pub const IPV4_ROUTE_PATH: &str = "/proc/net/route";
/// Path of the IPv6 routing table.
pub const IPV6_ROUTE_PATH: &str = "/proc/net/ipv6_route";

/// A system-assigned nameserver, like Go `netenv.Nameserver`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Nameserver {
    /// Server IP.
    #[serde(rename = "IP")]
    pub ip: IpAddr,
    /// Search domains assigned with the server.
    #[serde(rename = "Search")]
    pub search: Vec<String>,
}

impl Default for Nameserver {
    fn default() -> Self {
        Nameserver {
            ip: IpAddr::V4(Ipv4Addr::UNSPECIFIED),
            search: Vec::new(),
        }
    }
}

/// A default gateway with interface metadata, like Go `netenv.GatewayInfo`.
///
/// The mask is exposed as a dotted IPv4 netmask or an IPv6 CIDR prefix. Go
/// exposes the raw `net.IPMask` bytes; the semantic information is the same.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GatewayInfo {
    /// Gateway IP.
    #[serde(rename = "IP")]
    pub ip: IpAddr,
    /// Interface the route uses.
    #[serde(rename = "Interface")]
    pub interface: String,
    /// Netmask of the route.
    #[serde(rename = "Mask")]
    pub mask: String,
}

impl Default for GatewayInfo {
    fn default() -> Self {
        GatewayInfo {
            ip: IpAddr::V4(Ipv4Addr::UNSPECIFIED),
            interface: String::new(),
            mask: String::new(),
        }
    }
}

/// Access to the system sources for nameservers, gateways and connectivity.
pub trait EnvironmentSource: Send + Sync {
    /// Returns the configured nameservers, like Go `getNameserversFromDbus`
    /// followed by `getNameserversFromResolvconf`.
    fn nameservers(&self) -> Result<Vec<Nameserver>, String>;
    /// Returns the active default gateways, like Go `refreshGatewaysCache`.
    fn gateways(&self) -> Result<Vec<GatewayInfo>, String>;
    /// Returns the NetworkManager connectivity state, if available.
    fn connectivity_state(&self) -> Result<ConnectivityState, String>;
}

/// System implementation: files and D-Bus on Linux, PowerShell on Windows.
#[derive(Debug, Default, Clone, Copy)]
pub struct SystemEnvironment;

#[cfg(not(windows))]
impl EnvironmentSource for SystemEnvironment {
    fn nameservers(&self) -> Result<Vec<Nameserver>, String> {
        let mut nameservers = Vec::new();

        match nameservers_from_dbus(&SystemDbus) {
            Ok(found) => nameservers = add_nameservers(nameservers, found),
            Err(error) => tracing::warn!("netenv: could not get nameservers from dbus: {error}"),
        }

        match std::fs::read_to_string(RESOLV_CONF_PATH) {
            Ok(data) => match parse_resolvconf(&data) {
                Ok(found) => nameservers = add_nameservers(nameservers, found),
                Err(error) => {
                    tracing::warn!("netenv: could not parse /etc/resolv.conf: {error}")
                }
            },
            Err(error) => {
                tracing::warn!("netenv: could not read /etc/resolv.conf: {error}")
            }
        }

        Ok(nameservers)
    }

    fn gateways(&self) -> Result<Vec<GatewayInfo>, String> {
        let data = std::fs::read_to_string(IPV4_ROUTE_PATH)
            .map_err(|error| format!("could not read /proc/net/route: {error}"))?;
        let mut gateways = parse_ipv4_routes(&data)?;

        // IPv6 may simply not be configured; a missing file is not an error.
        if let Ok(data) = std::fs::read_to_string(IPV6_ROUTE_PATH) {
            gateways.extend(parse_ipv6_routes(&data));
        }
        Ok(gateways)
    }

    fn connectivity_state(&self) -> Result<ConnectivityState, String> {
        connectivity_state_from_dbus(&SystemDbus)
    }
}

/// Windows implementation using the default interface from PowerShell, like
/// Go `service/netenv/environment_windows.go`.
#[cfg(windows)]
impl EnvironmentSource for SystemEnvironment {
    fn nameservers(&self) -> Result<Vec<Nameserver>, String> {
        crate::windows_env::nameservers()
    }

    fn gateways(&self) -> Result<Vec<GatewayInfo>, String> {
        crate::windows_env::gateways()
    }

    fn connectivity_state(&self) -> Result<ConnectivityState, String> {
        Err("NetworkManager connectivity state is only available on Linux".to_string())
    }
}

/// Merges nameservers by IP, like Go `addNameservers`.
pub fn add_nameservers(
    mut nameservers: Vec<Nameserver>,
    new_nameservers: Vec<Nameserver>,
) -> Vec<Nameserver> {
    for new in new_nameservers {
        if nameservers.iter().all(|existing| existing.ip != new.ip) {
            nameservers.push(new);
        }
    }
    nameservers
}

/// Parses `/etc/resolv.conf`, like Go `getNameserversFromResolvconf`.
pub fn parse_resolvconf(data: &str) -> Result<Vec<Nameserver>, String> {
    let mut search_domains = Vec::new();
    let mut servers = Vec::new();

    for line in data.lines() {
        let mut fields = line.splitn(3, ' ');
        let Some(key) = fields.next() else {
            continue;
        };
        let Some(value) = fields.next() else {
            continue;
        };
        let value = value.trim();
        match key {
            "search" => {
                let fqdn = format!("{value}.");
                if is_valid_fqdn(&fqdn) {
                    search_domains.push(value.to_string());
                }
            }
            "nameserver" => {
                if let Ok(ip) = value.parse::<IpAddr>() {
                    servers.push(crate::interfaces::normalize_ip(ip));
                }
            }
            _ => {}
        }
    }

    Ok(servers
        .into_iter()
        .map(|ip| Nameserver {
            ip,
            search: search_domains.clone(),
        })
        .collect())
}

/// Parses the default routes of `/proc/net/route`.
pub fn parse_ipv4_routes(data: &str) -> Result<Vec<GatewayInfo>, String> {
    let mut gateways = Vec::new();
    for line in data.lines().skip(1) {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 4 || fields[1] != "00000000" {
            continue;
        }
        let gateway = match decode_le_ipv4(fields[2]) {
            Some(gateway) => gateway,
            None => {
                // One malformed line must not hide the remaining gateways.
                tracing::warn!(
                    raw = fields[2],
                    "netenv: could not parse gateway from /proc/net/route"
                );
                continue;
            }
        };
        let mask = fields
            .get(7)
            .and_then(|value| decode_le_ipv4(value))
            .map(|mask| mask.to_string())
            .unwrap_or_else(|| Ipv4Addr::UNSPECIFIED.to_string());
        gateways.push(GatewayInfo {
            ip: IpAddr::V4(gateway),
            interface: fields[0].to_string(),
            mask,
        });
    }
    Ok(gateways)
}

/// Parses the default routes of `/proc/net/ipv6_route`.
pub fn parse_ipv6_routes(data: &str) -> Vec<GatewayInfo> {
    const ZERO: &str = "00000000000000000000000000000000";
    let mut gateways = Vec::new();
    for line in data.lines() {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 6 || fields[0] != ZERO || fields[4] == ZERO {
            continue;
        }
        let Some(bytes) = decode_hex(fields[4], 16) else {
            continue;
        };
        let mut address = [0u8; 16];
        address.copy_from_slice(&bytes);
        let prefix = u8::from_str_radix(fields[1], 16).unwrap_or(0);
        gateways.push(GatewayInfo {
            ip: IpAddr::V6(Ipv6Addr::from(address)),
            interface: fields[fields.len() - 1].to_string(),
            mask: format!("/{prefix}"),
        });
    }
    gateways
}

fn decode_hex(value: &str, expected: usize) -> Option<Vec<u8>> {
    if value.len() != expected * 2 {
        return None;
    }
    (0..expected)
        .map(|index| u8::from_str_radix(&value[index * 2..index * 2 + 2], 16).ok())
        .collect()
}

fn decode_le_ipv4(value: &str) -> Option<Ipv4Addr> {
    let bytes = decode_hex(value, 4)?;
    Some(Ipv4Addr::new(bytes[3], bytes[2], bytes[1], bytes[0]))
}

/// Cached system environment, like the package-level caches in Go.
pub struct EnvironmentCache {
    source: Arc<dyn EnvironmentSource>,
    nameservers_flag: NetworkChangeFlag,
    gateways_flag: NetworkChangeFlag,
    // Go's broadcast flags start out set, so the first lookup always loads.
    nameservers_loaded: AtomicBool,
    gateways_loaded: AtomicBool,
    nameservers: RwLock<Vec<Nameserver>>,
    gateways: RwLock<Vec<GatewayInfo>>,
}

impl EnvironmentCache {
    /// Creates a cache with independent flags for nameservers and gateways.
    pub fn new(source: Arc<dyn EnvironmentSource>) -> EnvironmentCache {
        EnvironmentCache {
            source,
            nameservers_flag: network_changed_flag(),
            gateways_flag: network_changed_flag(),
            nameservers_loaded: AtomicBool::new(false),
            gateways_loaded: AtomicBool::new(false),
            nameservers: RwLock::new(Vec::new()),
            gateways: RwLock::new(Vec::new()),
        }
    }

    /// Returns the currently active nameservers, like Go `Nameservers`.
    pub fn nameservers(&self) -> Vec<Nameserver> {
        if self.nameservers_loaded.load(Ordering::SeqCst) && !self.nameservers_flag.is_set() {
            return self
                .nameservers
                .read()
                .unwrap_or_else(|error| error.into_inner())
                .clone();
        }
        self.nameservers_flag.refresh();
        self.nameservers_loaded.store(true, Ordering::SeqCst);

        match self.source.nameservers() {
            Ok(found) => {
                let mut cache = self
                    .nameservers
                    .write()
                    .unwrap_or_else(|error| error.into_inner());
                *cache = found;
            }
            Err(error) => tracing::warn!("netenv: failed to refresh nameservers: {error}"),
        }
        self.nameservers
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    /// Returns the currently active gateways with metadata, like Go
    /// `GatewaysInfo`.
    pub fn gateways_info(&self) -> Vec<GatewayInfo> {
        if self.gateways_loaded.load(Ordering::SeqCst) && !self.gateways_flag.is_set() {
            return self
                .gateways
                .read()
                .unwrap_or_else(|error| error.into_inner())
                .clone();
        }
        self.gateways_flag.refresh();
        self.gateways_loaded.store(true, Ordering::SeqCst);

        match self.source.gateways() {
            Ok(found) => {
                let mut cache = self
                    .gateways
                    .write()
                    .unwrap_or_else(|error| error.into_inner());
                *cache = found;
            }
            Err(error) => tracing::warn!("netenv: failed to refresh gateways: {error}"),
        }
        self.gateways
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    /// Returns the gateway IPs, like Go `Gateways`.
    pub fn gateways(&self) -> Vec<IpAddr> {
        self.gateways_info()
            .into_iter()
            .map(|gateway| gateway.ip)
            .collect()
    }

    /// Reads the connectivity state directly from the source.
    pub fn connectivity_state(&self) -> Result<ConnectivityState, String> {
        self.source.connectivity_state()
    }
}

static ENVIRONMENT: OnceLock<EnvironmentCache> = OnceLock::new();

/// Returns the process-wide environment cache.
pub fn environment() -> &'static EnvironmentCache {
    ENVIRONMENT.get_or_init(|| EnvironmentCache::new(Arc::new(SystemEnvironment)))
}

/// Returns the process-wide nameservers, like Go `netenv.Nameservers`.
pub fn nameservers() -> Vec<Nameserver> {
    environment().nameservers()
}

/// Returns the process-wide gateways, like Go `netenv.Gateways`.
pub fn gateways() -> Vec<IpAddr> {
    environment().gateways()
}

/// Returns the process-wide gateways with metadata, like Go
/// `netenv.GatewaysInfo`.
pub fn gateways_info() -> Vec<GatewayInfo> {
    environment().gateways_info()
}

/// Returns the NetworkManager connectivity state, like Go
/// `getConnectivityStateFromDbus`.
pub fn connectivity_state() -> Result<ConnectivityState, String> {
    environment().connectivity_state()
}
