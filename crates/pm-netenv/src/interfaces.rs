//! Network interface model and lookups, port of Go
//! `service/netenv/interfaces.go` and `interfaces_linux.go`.
//!
//! Interfaces are enumerated lazily and cached until the network change flag
//! fires; refreshes are throttled to one per second. Both facts are injected so
//! the logic is testable without real hardware: [`InterfaceSource`] provides
//! interfaces and routes.

use std::net::IpAddr;
#[cfg(unix)]
use std::net::{Ipv4Addr, Ipv6Addr};
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};

use pm_netstate::{ip_scope, IpScope};

use crate::NetworkChangeFlag;

/// A network interface as reported by the operating system.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct RawInterface {
    /// Interface name, e.g. `eth0`.
    pub name: String,
    /// Kernel interface index.
    pub index: u32,
    /// Hardware address in canonical lowercase colon form, if any.
    pub mac: Option<String>,
    /// Whether the interface is up.
    pub up: bool,
    /// Whether the interface is the loopback device.
    pub loopback: bool,
    /// Addresses with their prefix lengths.
    pub addrs: Vec<(IpAddr, u8)>,
}

/// A default route candidate: interface name and route metric.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RouteCandidate {
    /// Interface the route uses.
    pub interface: String,
    /// Route metric; lower wins.
    pub metric: u32,
}

/// Operating system access needed by the interface cache.
pub trait InterfaceSource: Send + Sync {
    /// Enumerates all network interfaces.
    fn interfaces(&self) -> Result<Vec<RawInterface>, String>;
    /// Returns whether the interface is backed by real hardware.
    fn is_physical(&self, name: &str) -> bool;
    /// Returns the default IPv4 routes, regardless of interface type.
    fn ipv4_default_routes(&self) -> Result<Vec<RouteCandidate>, String>;
    /// Returns the default IPv6 routes, regardless of interface type.
    ///
    /// An empty list is not an error: IPv6 may simply not be configured.
    fn ipv6_default_routes(&self) -> Vec<RouteCandidate>;
}

/// A network interface with its routable addresses.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct NetInterface {
    /// Interface name.
    pub name: String,
    /// Kernel interface index.
    pub index: u32,
    /// Canonical hardware address, or empty.
    pub mac: String,
    /// All routable addresses of the interface.
    pub addrs: Vec<IpAddr>,
}

/// A matched interface with the preferred address of each family.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct InterfaceInfo {
    /// The interface.
    pub interface: NetInterface,
    /// Preferred IPv4 address, if any.
    pub ipv4: Option<IpAddr>,
    /// Preferred IPv6 address, if any.
    pub ipv6: Option<IpAddr>,
}

/// The best physical interface per IP family.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct PhysicalDefaultInterfaces {
    pub for_ipv4: Option<InterfaceInfo>,
    pub for_ipv6: Option<InterfaceInfo>,
}

/// A cached interface with its precomputed family addresses.
#[derive(Debug, Clone, Default)]
struct CachedInterface {
    interface: NetInterface,
    first_ipv4: Option<IpAddr>,
    first_ipv6: Option<IpAddr>,
}

/// Lazily refreshed interface cache.
pub struct InterfaceCache {
    source: Arc<dyn InterfaceSource>,
    flag: NetworkChangeFlag,
    refresh_interval: Duration,
    cache: RwLock<CacheState>,
    physical: RwLock<PhysicalCacheState>,
}

#[derive(Default)]
struct CacheState {
    entries: Option<Vec<CachedInterface>>,
    last_refresh: Option<Instant>,
    last_error: Option<String>,
}

#[derive(Default)]
struct PhysicalCacheState {
    result: Option<PhysicalDefaultInterfaces>,
    last_refresh: Option<Instant>,
}

impl InterfaceCache {
    /// Creates a cache with the default one-second refresh throttle.
    pub fn new(source: Arc<dyn InterfaceSource>, flag: NetworkChangeFlag) -> InterfaceCache {
        InterfaceCache::with_refresh_interval(source, flag, Duration::from_secs(1))
    }

    /// Creates a cache with an explicit refresh throttle; zero refreshes on
    /// every network change notification.
    pub fn with_refresh_interval(
        source: Arc<dyn InterfaceSource>,
        flag: NetworkChangeFlag,
        refresh_interval: Duration,
    ) -> InterfaceCache {
        InterfaceCache {
            source,
            flag,
            refresh_interval,
            cache: RwLock::new(CacheState::default()),
            physical: RwLock::new(PhysicalCacheState::default()),
        }
    }

    /// Refreshes the cache if needed.
    fn ensure(&self) -> Result<(), String> {
        if self.is_ready() {
            return Ok(());
        }

        let mut state = self.cache.write().unwrap_or_else(|error| error.into_inner());
        if state.entries.is_some() && !self.needs_refresh() {
            return Ok(());
        }

        // Throttle repeated refreshes.
        if state.entries.is_some()
            && state
                .last_refresh
                .is_some_and(|last| last.elapsed() < self.refresh_interval)
        {
            if let Some(error) = &state.last_error {
                return Err(format!(
                    "failed to previously refresh interface cache: {error}"
                ));
            }
            return Ok(());
        }

        self.flag.refresh();
        let interfaces = match self.source.interfaces() {
            Ok(interfaces) => interfaces,
            Err(error) => {
                state.last_error = Some(error.clone());
                state.last_refresh = Some(Instant::now());
                return Err(format!("failed to enumerate network interfaces: {error}"));
            }
        };

        let mut entries = Vec::with_capacity(interfaces.len());
        for interface in interfaces {
            if !interface.up || interface.loopback {
                continue;
            }

            let mut addrs = Vec::new();
            for (ip, _prefix) in &interface.addrs {
                let ip = normalize_ip(*ip);
                if !is_routable_unicast(ip) {
                    continue;
                }
                addrs.push(ip);
            }
            if addrs.is_empty() {
                continue;
            }

            let first_ipv4 = addrs.iter().copied().find(IpAddr::is_ipv4);
            let first_ipv6 = addrs.iter().copied().find(IpAddr::is_ipv6);
            entries.push(CachedInterface {
                interface: NetInterface {
                    name: interface.name,
                    index: interface.index,
                    mac: interface.mac.unwrap_or_default(),
                    addrs,
                },
                first_ipv4,
                first_ipv6,
            });
        }

        state.entries = Some(entries);
        state.last_error = None;
        state.last_refresh = Some(Instant::now());
        Ok(())
    }

    /// Returns whether the cache is populated and current.
    fn is_ready(&self) -> bool {
        let state = self.cache.read().unwrap_or_else(|error| error.into_inner());
        state.entries.is_some() && !self.needs_refresh()
    }

    fn needs_refresh(&self) -> bool {
        self.flag.is_set()
    }

    /// Returns the interface identified by an IP, MAC or name, tried in that
    /// order, like Go `GetInterface`.
    pub fn get_interface(&self, ifinfo: &str) -> Result<InterfaceInfo, String> {
        self.ensure()?;
        let entries = self.snapshot();

        if let Ok(ip) = ifinfo.parse::<IpAddr>() {
            let ip = normalize_ip(ip);
            let Some(entry) = search_by_ip(&entries, ip) else {
                return Err(format!("no interface found {ifinfo:?}"));
            };
            return Ok(build_info(entry, Some(ip)));
        }
        if let Some(mac) = canonical_mac(ifinfo) {
            let Some(entry) = search_by_mac(&entries, &mac) else {
                return Err(format!("no interface found {ifinfo:?}"));
            };
            return Ok(build_info(entry, None));
        }
        let Some(entry) = search_by_name(&entries, ifinfo) else {
            return Err(format!("no interface found {ifinfo:?}"));
        };
        Ok(build_info(entry, None))
    }

    /// Returns the interface that has the given IP assigned.
    pub fn get_interface_by_ip(&self, ip: IpAddr) -> Result<InterfaceInfo, String> {
        self.ensure()?;
        let ip = normalize_ip(ip);
        let entries = self.snapshot();
        let Some(entry) = search_by_ip(&entries, ip) else {
            return Err(format!("no interface found with IP {ip}"));
        };
        Ok(build_info(entry, Some(ip)))
    }

    /// Returns the interface with the given MAC address.
    pub fn get_interface_by_mac(&self, mac: &str) -> Result<InterfaceInfo, String> {
        let Some(mac) = canonical_mac(mac) else {
            return Err(format!("no interface found with MAC {mac}"));
        };
        self.ensure()?;
        let entries = self.snapshot();
        let Some(entry) = search_by_mac(&entries, &mac) else {
            return Err(format!("no interface found with MAC {mac}"));
        };
        Ok(build_info(entry, None))
    }

    /// Returns the interface with the given name.
    pub fn get_interface_by_name(&self, name: &str) -> Result<InterfaceInfo, String> {
        self.ensure()?;
        let entries = self.snapshot();
        let Some(entry) = search_by_name(&entries, name) else {
            return Err(format!("no interface found with name {name:?}"));
        };
        Ok(build_info(entry, None))
    }

    /// Returns the best physical interface per IP family, like Go
    /// `GetBestPhysicalDefaultInterfaces`.
    pub fn best_physical_default_interfaces(
        &self,
    ) -> Result<PhysicalDefaultInterfaces, String> {
        {
            let physical = self
                .physical
                .read()
                .unwrap_or_else(|error| error.into_inner());
            if let Some(result) = &physical.result {
                if !self.needs_refresh() {
                    return Ok(result.clone());
                }
            }
        }

        let mut physical = self
            .physical
            .write()
            .unwrap_or_else(|error| error.into_inner());
        if let Some(result) = &physical.result {
            if !self.needs_refresh() {
                return Ok(result.clone());
            }
            if physical
                .last_refresh
                .is_some_and(|last| last.elapsed() < self.refresh_interval)
            {
                return Ok(result.clone());
            }
        }

        self.ensure()?;
        physical.last_refresh = Some(Instant::now());
        self.flag.refresh();

        let ipv4_routes = self
            .source
            .ipv4_default_routes()
            .map_err(|error| format!("reading IPv4 routing table: {error}"))?;
        let ipv6_routes = self.source.ipv6_default_routes();
        let entries = self.snapshot();

        let for_ipv4 = pick_route(
            &ipv4_routes,
            &entries,
            self.source.as_ref(),
            |info| info.ipv4.is_some(),
        );
        let for_ipv6 = pick_route(
            &ipv6_routes,
            &entries,
            self.source.as_ref(),
            |info| info.ipv6.is_some(),
        );

        let result = PhysicalDefaultInterfaces {
            for_ipv4,
            for_ipv6,
        };
        physical.result = Some(result.clone());
        Ok(result)
    }

    fn snapshot(&self) -> Vec<CachedInterface> {
        self.cache
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .entries
            .clone()
            .unwrap_or_default()
    }
}

fn pick_route(
    routes: &[RouteCandidate],
    entries: &[CachedInterface],
    source: &dyn InterfaceSource,
    has_family: impl Fn(&InterfaceInfo) -> bool,
) -> Option<InterfaceInfo> {
    let mut best: Option<(u32, InterfaceInfo)> = None;
    for route in routes {
        if !source.is_physical(&route.interface) {
            continue;
        }
        let Some(entry) = search_by_name(entries, &route.interface) else {
            continue;
        };
        let info = build_info(entry, None);
        if !has_family(&info) {
            continue;
        }
        if best.as_ref().is_none_or(|(metric, _)| route.metric < *metric) {
            best = Some((route.metric, info));
        }
    }
    best.map(|(_, info)| info)
}

fn build_info(entry: &CachedInterface, known_ip: Option<IpAddr>) -> InterfaceInfo {
    let mut info = InterfaceInfo {
        interface: entry.interface.clone(),
        ipv4: entry.first_ipv4,
        ipv6: entry.first_ipv6,
    };
    if let Some(ip) = known_ip {
        if ip.is_ipv4() {
            info.ipv4 = Some(ip);
        } else {
            info.ipv6 = Some(ip);
        }
    }
    info
}

fn search_by_ip(entries: &[CachedInterface], ip: IpAddr) -> Option<&CachedInterface> {
    entries
        .iter()
        .find(|entry| entry.interface.addrs.contains(&ip))
}

fn search_by_mac<'a>(entries: &'a [CachedInterface], mac: &str) -> Option<&'a CachedInterface> {
    entries
        .iter()
        .find(|entry| entry.interface.mac == mac)
}

fn search_by_name<'a>(entries: &'a [CachedInterface], name: &str) -> Option<&'a CachedInterface> {
    entries
        .iter()
        .find(|entry| entry.interface.name == name)
}

/// Returns the 4-byte form of IPv4 and the address itself for IPv6.
pub fn normalize_ip(ip: IpAddr) -> IpAddr {
    match ip {
        IpAddr::V6(ip6) => match ip6.to_ipv4_mapped() {
            Some(ip4) => IpAddr::V4(ip4),
            None => IpAddr::V6(ip6),
        },
        ip => ip,
    }
}

/// Returns whether the IP is a routable unicast address (site-local or global).
pub fn is_routable_unicast(ip: IpAddr) -> bool {
    matches!(ip_scope(ip), IpScope::SiteLocal | IpScope::Global)
}

/// Parses a MAC address and returns the canonical lowercase colon form.
///
/// Accepts colon, hyphen and dotted groups of two hex digits.
pub fn canonical_mac(value: &str) -> Option<String> {
    let value = value.trim();
    if value.is_empty() {
        return None;
    }

    let hex: String = if value.contains(':') || value.contains('-') {
        value
            .chars()
            .filter(|char| *char != ':' && *char != '-')
            .collect()
    } else if value.contains('.') {
        value.chars().filter(|char| *char != '.').collect()
    } else {
        return None;
    };

    // Go `net.ParseMAC` accepts 6, 8 and 20 byte addresses.
    if !matches!(hex.len(), 12 | 16 | 40)
        || !hex.chars().all(|char| char.is_ascii_hexdigit())
    {
        return None;
    }
    let bytes: Vec<String> = hex
        .as_bytes()
        .chunks(2)
        .map(|chunk| String::from_utf8_lossy(chunk).to_lowercase())
        .collect();
    Some(bytes.join(":"))
}

/// Returns whether an IP is in the given network.
pub fn network_contains(network: IpAddr, prefix_len: u8, ip: IpAddr) -> bool {
    match (normalize_ip(network), normalize_ip(ip)) {
        (IpAddr::V4(network), IpAddr::V4(ip)) => {
            if prefix_len > 32 {
                return false;
            }
            let mask = if prefix_len == 0 {
                0
            } else {
                u32::MAX << (32 - u32::from(prefix_len))
            };
            (u32::from(network) & mask) == (u32::from(ip) & mask)
        }
        (IpAddr::V6(network), IpAddr::V6(ip)) => {
            if prefix_len > 128 {
                return false;
            }
            let network = u128::from(network);
            let ip = u128::from(ip);
            let mask = if prefix_len == 0 {
                0
            } else {
                u128::MAX << (128 - u32::from(prefix_len))
            };
            (network & mask) == (ip & mask)
        }
        _ => false,
    }
}

/// Returns the prefix length of an IPv4 netmask.
#[cfg(unix)]
pub(crate) fn mask_prefix_v4(mask: Ipv4Addr) -> u8 {
    u32::from(mask).leading_ones() as u8
}

/// Returns the prefix length of an IPv6 netmask.
#[cfg(unix)]
pub(crate) fn mask_prefix_v6(mask: Ipv6Addr) -> u8 {
    u128::from(mask).leading_ones() as u8
}
