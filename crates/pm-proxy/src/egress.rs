//! Tracking of active proxy egress destinations.
//!
//! The split tunnel firewall check needs to know whether the core's own
//! connection is the egress of a proxy session (to the destination itself or to
//! an upstream proxy server), like Go's proxy session cache.

use std::collections::HashMap;
use std::net::{IpAddr, SocketAddr};
use std::sync::Mutex;

/// Egress addresses of active proxy sessions.
#[derive(Debug, Default)]
pub struct EgressCache {
    entries: Mutex<HashMap<SocketAddr, usize>>,
}

impl EgressCache {
    /// Creates an empty cache.
    pub fn new() -> EgressCache {
        EgressCache::default()
    }

    /// Registers one more session for the egress address.
    pub fn add(&self, address: SocketAddr) {
        let mut entries = self.entries.lock().unwrap_or_else(|error| error.into_inner());
        *entries.entry(address).or_insert(0) += 1;
    }

    /// Removes one session for the egress address.
    pub fn remove(&self, address: SocketAddr) {
        let mut entries = self.entries.lock().unwrap_or_else(|error| error.into_inner());
        match entries.get_mut(&address) {
            Some(count) if *count > 1 => *count -= 1,
            Some(_) => {
                entries.remove(&address);
            }
            None => {}
        }
    }

    /// Returns whether any session egresses to the address.
    pub fn contains(&self, address: SocketAddr) -> bool {
        self.entries
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .contains_key(&address)
    }

    /// Returns whether any session egresses to the IP and port.
    pub fn has(&self, ip: IpAddr, port: u16) -> bool {
        self.contains(SocketAddr::new(ip, port))
    }

    /// Returns the number of active egress entries.
    pub fn len(&self) -> usize {
        self.entries
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .len()
    }

    /// Returns whether no sessions are tracked.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

/// Removes a session from the cache when it is dropped.
pub struct EgressGuard {
    cache: std::sync::Arc<EgressCache>,
    address: SocketAddr,
}

impl EgressGuard {
    /// Registers the egress address until the guard is dropped.
    pub fn new(cache: std::sync::Arc<EgressCache>, address: SocketAddr) -> EgressGuard {
        cache.add(address);
        EgressGuard { cache, address }
    }
}

impl Drop for EgressGuard {
    fn drop(&mut self) {
        self.cache.remove(self.address);
    }
}
