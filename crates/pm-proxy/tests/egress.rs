//! Tests for the proxy egress cache.

use std::net::SocketAddr;
use std::sync::Arc;

use pm_proxy::{EgressCache, EgressGuard};

#[test]
fn counts_references() {
    let cache = EgressCache::new();
    let address: SocketAddr = "1.1.1.1:443".parse().unwrap();

    assert!(!cache.contains(address));
    cache.add(address);
    assert!(cache.contains(address));
    cache.add(address);
    cache.remove(address);
    assert!(cache.contains(address), "one session is still active");
    cache.remove(address);
    assert!(!cache.contains(address));
    // Removing an unknown address is harmless.
    cache.remove(address);
    assert!(cache.is_empty());
}

#[test]
fn guard_removes_the_session_on_drop() {
    let cache = Arc::new(EgressCache::new());
    let address: SocketAddr = "1.1.1.1:443".parse().unwrap();

    {
        let _guard = EgressGuard::new(Arc::clone(&cache), address);
        assert!(cache.has(address.ip(), address.port()));
        assert_eq!(cache.len(), 1);
    }
    assert!(!cache.has(address.ip(), address.port()));
    assert!(cache.is_empty());
}

#[test]
fn ipv6_addresses_are_tracked() {
    let cache = EgressCache::new();
    let address: SocketAddr = "[2001:db8::1]:80".parse().unwrap();
    cache.add(address);
    assert!(cache.has("2001:db8::1".parse().unwrap(), 80));
    assert!(!cache.has("2001:db8::1".parse().unwrap(), 81));
}
