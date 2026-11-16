//! Port of the registry part of Go `preauth.go`: local ports that are
//! pre-authenticated and attributed to the Portmaster itself.
//!
//! Selecting an unused local port (`network.GetUnusedLocalPort`) and the
//! resolver/netenv hooks are not part of this crate.

use std::collections::HashSet;
use std::sync::Mutex;

pub const PROTOCOL_TCP: u8 = 6;
pub const PROTOCOL_UDP: u8 = 17;

/// Maps the network names `"tcp"` and `"udp"` to protocol numbers.
pub fn protocol_from_network(network: &str) -> Option<u8> {
    match network {
        "udp" => Some(PROTOCOL_UDP),
        "tcp" => Some(PROTOCOL_TCP),
        _ => None,
    }
}

/// Set of pre-authenticated `(protocol, port)` pairs; every entry is valid once.
#[derive(Debug, Default)]
pub struct PreAuthPorts {
    ports: Mutex<HashSet<(u8, u16)>>,
}

impl PreAuthPorts {
    pub fn new() -> PreAuthPorts {
        PreAuthPorts::default()
    }

    pub fn register(&self, protocol: u8, port: u16) {
        self.ports
            .lock()
            .expect("lock poisoned")
            .insert((protocol, port));
    }

    pub fn unregister(&self, protocol: u8, port: u16) {
        self.ports
            .lock()
            .expect("lock poisoned")
            .remove(&(protocol, port));
    }

    /// Checks and immediately removes the pre-authentication.
    pub fn is_pre_authenticated(&self, protocol: u8, port: u16) -> bool {
        self.ports
            .lock()
            .expect("lock poisoned")
            .remove(&(protocol, port))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pre_auth_is_valid_once() {
        let ports = PreAuthPorts::new();
        ports.register(PROTOCOL_UDP, 5000);
        assert!(!ports.is_pre_authenticated(PROTOCOL_TCP, 5000));
        assert!(ports.is_pre_authenticated(PROTOCOL_UDP, 5000));
        assert!(!ports.is_pre_authenticated(PROTOCOL_UDP, 5000));
    }

    #[test]
    fn unregister_removes() {
        let ports = PreAuthPorts::new();
        ports.register(PROTOCOL_TCP, 1);
        ports.unregister(PROTOCOL_TCP, 1);
        assert!(!ports.is_pre_authenticated(PROTOCOL_TCP, 1));
    }

    #[test]
    fn network_names() {
        assert_eq!(protocol_from_network("tcp"), Some(6));
        assert_eq!(protocol_from_network("udp"), Some(17));
        assert_eq!(protocol_from_network("tcp4"), None);
    }
}
