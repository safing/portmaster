//! The network environment the resolver depends on (Go `netenv`), injectable for tests.

use std::net::IpAddr;
use std::sync::Mutex;

use pm_netenv::OnlineStatus;

/// Domain used to test whether a resolver (and the device) is online.
pub const DNS_TEST_DOMAIN: &str = "online-check.safing.io.";

/// Domains used by connectivity checks, copied verbatim from Go
/// `netenv.ConnectivityDomains` (including the entry without trailing dot).
pub const CONNECTIVITY_DOMAINS: &[&str] = &[
    pm_netenv::SPECIAL_CAPTIVE_PORTAL_DOMAIN,
    // Windows
    "dns.msftncsi.com.",
    "msftncsi.com.",
    "www.msftncsi.com.",
    "microsoftconnecttest.com.",
    "www.microsoftconnecttest.com.",
    "ipv6.microsoftconnecttest.com.",
    // Apple
    "captive.apple.com.",
    // Linux
    "connectivity-check.ubuntu.com.",
    "nmcheck.gnome.org.",
    "network-test.debian.org.",
    "204.pop-os.org.",
    "conncheck.opensuse.org.",
    "ping.archlinux.org",
    // Android
    "connectivitycheck.gstatic.com.",
    // Other
    "neverssl.com.",
    "detectportal.firefox.com.",
];

/// A nameserver assigned by the system or network.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Nameserver {
    pub ip: IpAddr,
    pub search: Vec<String>,
}

/// The facts about the network that the resolver needs.
pub trait Environment: Send + Sync {
    /// The current online status.
    fn online_status(&self) -> OnlineStatus;

    /// Whether the domain (FQDN) is used for connectivity checks and must be
    /// resolved by the network assigned DNS server.
    fn is_connectivity_domain(&self, fqdn: &str) -> bool;

    /// The nameservers assigned by the system or network.
    fn nameservers(&self) -> Vec<Nameserver>;

    /// Whether the IP is in a network the device is directly attached to.
    fn is_local_network(&self, ip: IpAddr) -> bool;

    /// Reports whether a configured server answered (`true`) or failed (`false`).
    fn set_connected_to_dns(&self, _connected: bool) {}

    /// Called when all configured resolvers are failing (`true`) or work again (`false`).
    fn failing_resolvers_changed(&self, _all_failing: bool) {}

    /// Whether the device is online or semi-online.
    fn online(&self) -> bool {
        self.online_status().is_online()
    }
}

/// Parses nameservers and search domains from `resolv.conf` content, like the
/// Go Linux fallback: every nameserver gets all search domains.
pub fn parse_resolv_conf(content: &str) -> Vec<Nameserver> {
    let mut search_domains: Vec<String> = Vec::new();
    let mut servers: Vec<IpAddr> = Vec::new();

    for line in content.lines() {
        // Go: strings.SplitN(line, " ", 3); needs at least two parts.
        let mut parts = line.splitn(3, ' ');
        let (Some(key), Some(value)) = (parts.next(), parts.next()) else {
            continue;
        };
        match key {
            "search" => {
                let fqdn = if value.ends_with('.') {
                    value.to_string()
                } else {
                    format!("{value}.")
                };
                if pm_netutils::is_valid_fqdn(&fqdn) {
                    search_domains.push(value.to_string());
                }
            }
            "nameserver" => {
                if let Ok(ip) = value.parse::<IpAddr>() {
                    servers.push(ip.to_canonical());
                }
            }
            _ => {}
        }
    }

    servers
        .into_iter()
        .map(|ip| Nameserver {
            ip,
            search: search_domains.clone(),
        })
        .collect()
}

/// Removes nameservers with an IP that is already present, like Go `addNameservers`.
pub fn dedupe_nameservers(list: Vec<Nameserver>) -> Vec<Nameserver> {
    let mut out: Vec<Nameserver> = Vec::new();
    for ns in list {
        if !out.iter().any(|existing| existing.ip == ns.ip) {
            out.push(ns);
        }
    }
    out
}

/// Reads the on-link IPv4 networks from `/proc/net/route` content.
pub fn parse_proc_net_route(content: &str) -> Vec<(u32, u32)> {
    let mut networks = Vec::new();
    for line in content.lines().skip(1) {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 8 {
            continue;
        }
        // Values are in host byte order; on-link routes have no gateway.
        let (Ok(dest), Ok(gateway), Ok(mask)) = (
            u32::from_str_radix(fields[1], 16),
            u32::from_str_radix(fields[2], 16),
            u32::from_str_radix(fields[7], 16),
        ) else {
            continue;
        };
        if gateway == 0 && dest != 0 {
            networks.push((dest.to_be(), mask.to_be()));
        }
    }
    networks
}

/// The environment of the running system: `pm-netenv` for the online state,
/// `/etc/resolv.conf` for nameservers and `/proc/net/route` for local networks.
///
/// Nameservers from D-Bus (NetworkManager) and Windows are not read; local
/// IPv6 networks are not detected.
#[derive(Debug, Default)]
pub struct SystemEnvironment;

impl Environment for SystemEnvironment {
    fn online_status(&self) -> OnlineStatus {
        pm_netenv::get_online_status()
    }

    fn is_connectivity_domain(&self, fqdn: &str) -> bool {
        if fqdn.is_empty() {
            return false;
        }
        if CONNECTIVITY_DOMAINS.contains(&fqdn) {
            return true;
        }
        pm_netenv::get_captive_portal()
            .is_some_and(|portal| !portal.domain.is_empty() && portal.domain == fqdn)
    }

    fn nameservers(&self) -> Vec<Nameserver> {
        match std::fs::read_to_string("/etc/resolv.conf") {
            Ok(content) => dedupe_nameservers(parse_resolv_conf(&content)),
            Err(err) => {
                tracing::warn!("resolver: could not read /etc/resolv.conf: {err}");
                Vec::new()
            }
        }
    }

    fn is_local_network(&self, ip: IpAddr) -> bool {
        let IpAddr::V4(v4) = ip else {
            return false;
        };
        let Ok(content) = std::fs::read_to_string("/proc/net/route") else {
            return false;
        };
        let ip = u32::from(v4);
        parse_proc_net_route(&content)
            .into_iter()
            .any(|(dest, mask)| ip & mask == dest & mask)
    }
}

/// A scriptable environment for tests.
pub struct StaticEnvironment {
    state: Mutex<StaticEnvironmentState>,
}

/// The state of a [`StaticEnvironment`].
#[derive(Debug, Clone)]
pub struct StaticEnvironmentState {
    pub online_status: OnlineStatus,
    pub connectivity_domains: Vec<String>,
    pub nameservers: Vec<Nameserver>,
    pub local_networks: Vec<(IpAddr, u8)>,
    pub connected_to_dns: Option<bool>,
    pub failing_notifications: Vec<bool>,
}

impl Default for StaticEnvironment {
    fn default() -> Self {
        StaticEnvironment {
            state: Mutex::new(StaticEnvironmentState {
                online_status: OnlineStatus::Online,
                connectivity_domains: Vec::new(),
                nameservers: Vec::new(),
                local_networks: Vec::new(),
                connected_to_dns: None,
                failing_notifications: Vec::new(),
            }),
        }
    }
}

impl StaticEnvironment {
    /// Changes the state.
    pub fn update<R>(&self, change: impl FnOnce(&mut StaticEnvironmentState) -> R) -> R {
        change(&mut self.state.lock().expect("lock poisoned"))
    }

    /// Reads the state.
    pub fn read<R>(&self, read: impl FnOnce(&StaticEnvironmentState) -> R) -> R {
        read(&self.state.lock().expect("lock poisoned"))
    }
}

fn in_network(ip: IpAddr, network: IpAddr, prefix: u8) -> bool {
    match (ip, network) {
        (IpAddr::V4(ip), IpAddr::V4(net)) => {
            let mask = if prefix == 0 {
                0
            } else {
                u32::MAX << (32 - u32::from(prefix.min(32)))
            };
            u32::from(ip) & mask == u32::from(net) & mask
        }
        (IpAddr::V6(ip), IpAddr::V6(net)) => {
            let mask = if prefix == 0 {
                0
            } else {
                u128::MAX << (128 - u32::from(prefix.min(128)))
            };
            u128::from(ip) & mask == u128::from(net) & mask
        }
        _ => false,
    }
}

impl Environment for StaticEnvironment {
    fn online_status(&self) -> OnlineStatus {
        self.read(|s| s.online_status)
    }

    fn is_connectivity_domain(&self, fqdn: &str) -> bool {
        !fqdn.is_empty() && self.read(|s| s.connectivity_domains.iter().any(|d| d == fqdn))
    }

    fn nameservers(&self) -> Vec<Nameserver> {
        self.read(|s| s.nameservers.clone())
    }

    fn is_local_network(&self, ip: IpAddr) -> bool {
        self.read(|s| {
            s.local_networks
                .iter()
                .any(|(net, prefix)| in_network(ip, *net, *prefix))
        })
    }

    fn set_connected_to_dns(&self, connected: bool) {
        self.update(|s| s.connected_to_dns = Some(connected));
    }

    fn failing_resolvers_changed(&self, all_failing: bool) {
        self.update(|s| s.failing_notifications.push(all_failing));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resolv_conf_parsing_matches_go_fallback() {
        let content = "# comment\nsearch lan.example corp.example\nnameserver 192.168.1.1\nnameserver 2001:db8::1\nnameserver bogus\nnameserver ::ffff:10.0.0.1\noptions edns0\nsearch\n";
        let servers = parse_resolv_conf(content);
        assert_eq!(servers.len(), 3);
        // Go splits at the first two spaces: only the first search domain is read.
        assert_eq!(servers[0].search, vec!["lan.example".to_string()]);
        assert_eq!(servers[0].ip, "192.168.1.1".parse::<IpAddr>().unwrap());
        assert_eq!(servers[2].ip, "10.0.0.1".parse::<IpAddr>().unwrap());
    }

    #[test]
    fn resolv_conf_search_domains_accumulate() {
        let servers =
            parse_resolv_conf("search a.example\nsearch b.example\nnameserver 10.0.0.2\n");
        assert_eq!(
            servers[0].search,
            vec!["a.example".to_string(), "b.example".to_string()]
        );
    }

    #[test]
    fn nameservers_are_deduplicated_by_ip() {
        let ns = |ip: &str| Nameserver {
            ip: ip.parse().unwrap(),
            search: vec![],
        };
        let out = dedupe_nameservers(vec![ns("10.0.0.1"), ns("10.0.0.2"), ns("10.0.0.1")]);
        assert_eq!(out.len(), 2);
    }

    #[test]
    fn route_table_parsing() {
        let route =
            "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n\
            eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n\
            eth0\t0001A8C0\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n";
        let networks = parse_proc_net_route(route);
        assert_eq!(networks, vec![(0xC0A8_0100, 0xFFFF_FF00)]);
    }

    #[test]
    fn connectivity_domains_are_verbatim() {
        assert_eq!(CONNECTIVITY_DOMAINS.len(), 17);
        assert!(CONNECTIVITY_DOMAINS.contains(&"ping.archlinux.org"));
    }
}
