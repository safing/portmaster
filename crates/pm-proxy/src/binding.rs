use std::net::IpAddr;
use std::sync::Arc;
use crate::upstream::UpstreamProxy;

/// LocalBinding carries the local-side binding parameters for an outbound proxy connection.
#[derive(Debug, Clone, Default)]
pub struct LocalBinding {
    /// IP is the local source address to bind the outgoing socket to.
    pub ip: Option<IpAddr>,

    /// Interface is the name of the network interface (e.g. "eth0") to bind
    /// the outgoing socket to.
    pub interface: Option<String>,

    /// AltIP is an optional local source address of the other IP version than IP.
    /// It is used instead of IP when the remote address is of that IP version.
    pub alt_ip: Option<IpAddr>,

    /// Upstream, if set, relays the outbound TCP connection through a proxy server
    /// instead of connecting to the destination directly.
    pub upstream: Option<Arc<UpstreamProxy>>,
}

impl LocalBinding {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with_ip(mut self, ip: IpAddr) -> Self {
        self.ip = Some(ip);
        self
    }

    pub fn with_interface(mut self, iface: impl Into<String>) -> Self {
        self.interface = Some(iface.into());
        self
    }

    pub fn with_alt_ip(mut self, alt_ip: IpAddr) -> Self {
        self.alt_ip = Some(alt_ip);
        self
    }

    pub fn with_upstream(mut self, upstream: UpstreamProxy) -> Self {
        self.upstream = Some(Arc::new(upstream));
        self
    }
}
