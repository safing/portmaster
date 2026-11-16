//! The nameserver IP matcher for the firewall fast-track (Go
//! `firewall.SetNameserverIPMatcher`).

use std::net::IpAddr;
use std::sync::Arc;

use crate::env::ServerEnv;

/// Tells whether an address is the local DNS server.
#[derive(Clone)]
pub struct NameserverIpMatcher {
    ips: Vec<IpAddr>,
    /// Listening on all addresses: any local IP is ours.
    any_local: Option<Arc<dyn ServerEnv>>,
    port: u16,
}

impl NameserverIpMatcher {
    pub(crate) fn new(
        ips: Vec<IpAddr>,
        any_local: Option<Arc<dyn ServerEnv>>,
        port: u16,
    ) -> NameserverIpMatcher {
        NameserverIpMatcher {
            ips,
            any_local,
            port,
        }
    }

    /// The Go matcher: only the IP is compared.
    pub fn matches_ip(&self, ip: IpAddr) -> bool {
        if let Some(env) = &self.any_local {
            return match env.is_my_ip(ip) {
                Ok(mine) => mine,
                Err(err) => {
                    tracing::warn!("nameserver: failed to check if IP {ip} is local: {err}");
                    false
                }
            };
        }
        let ip = ip.to_canonical();
        self.ips.iter().any(|ours| ours.to_canonical() == ip)
    }

    /// Whether `ip:port` is the local DNS server.
    pub fn matches(&self, ip: IpAddr, port: u16) -> bool {
        port == self.port && self.matches_ip(ip)
    }

    /// The listening port.
    pub fn port(&self) -> u16 {
        self.port
    }
}
