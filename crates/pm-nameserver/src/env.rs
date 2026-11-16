//! The environment of the nameserver: facts that Go reads from `netenv`,
//! `core` and the operating system.

use std::net::{IpAddr, SocketAddr, UdpSocket};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

/// Environment queries of the nameserver. Injectable for tests.
pub trait ServerEnv: Send + Sync {
    /// The hostname with a trailing dot, like Go builds it in `start`.
    fn hostname(&self) -> String;

    /// Whether the IP is one of this device (Go `netenv.IsMyIP`).
    fn is_my_ip(&self, ip: IpAddr) -> Result<bool, String>;

    /// Whether requests from other devices are served
    /// (`core/networkService`).
    fn network_service_mode(&self) -> bool;

    /// Whether the device has an IPv6 stack (Go `netenv.IPv6Enabled`).
    fn ipv6_enabled(&self) -> bool;

    /// Returns true once after the network changed, like refreshing a
    /// `netenv.GetNetworkChangedFlag`.
    fn take_network_changed(&self) -> bool {
        false
    }
}

/// [`ServerEnv`] for the running system.
pub struct SystemEnv {
    network_service: Arc<dyn Fn() -> bool + Send + Sync>,
    network_changed: AtomicBool,
}

impl SystemEnv {
    /// `network_service` reads the `core/networkService` option.
    pub fn new(network_service: impl Fn() -> bool + Send + Sync + 'static) -> SystemEnv {
        SystemEnv {
            network_service: Arc::new(network_service),
            network_changed: AtomicBool::new(false),
        }
    }

    /// Marks the network as changed; the next check resets failing queries.
    pub fn set_network_changed(&self) {
        self.network_changed.store(true, Ordering::SeqCst);
    }
}

impl ServerEnv for SystemEnv {
    fn hostname(&self) -> String {
        let name = std::fs::read_to_string("/proc/sys/kernel/hostname")
            .ok()
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .or_else(|| std::env::var("HOSTNAME").ok())
            .or_else(|| std::env::var("COMPUTERNAME").ok())
            .unwrap_or_default();
        if name.is_empty() {
            tracing::warn!("nameserver: failed to get hostname");
        }
        format!("{name}.")
    }

    /// An IP is this device's if a socket can be bound to it. Loopback is
    /// always local and multicast never is, like in Go.
    fn is_my_ip(&self, ip: IpAddr) -> Result<bool, String> {
        let ip = ip.to_canonical();
        if ip.is_loopback() {
            return Ok(true);
        }
        if ip.is_multicast() {
            return Ok(false);
        }
        Ok(UdpSocket::bind(SocketAddr::new(ip, 0)).is_ok())
    }

    fn network_service_mode(&self) -> bool {
        (self.network_service)()
    }

    fn ipv6_enabled(&self) -> bool {
        UdpSocket::bind("[::1]:0").is_ok()
    }

    fn take_network_changed(&self) -> bool {
        self.network_changed.swap(false, Ordering::SeqCst)
    }
}
