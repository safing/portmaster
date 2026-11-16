//! Tests for the local address cache (Go `netenv/adresses.go`).

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_netenv::{
    AddressCache, FlagPair, InterfaceSource, RawInterface, RouteCandidate,
};

struct FakeSource {
    interfaces: Mutex<Vec<RawInterface>>,
    calls: AtomicUsize,
    fail: Mutex<Option<String>>,
}

impl FakeSource {
    fn new(interfaces: Vec<RawInterface>) -> Arc<FakeSource> {
        Arc::new(FakeSource {
            interfaces: Mutex::new(interfaces),
            calls: AtomicUsize::new(0),
            fail: Mutex::new(None),
        })
    }
}

impl InterfaceSource for FakeSource {
    fn interfaces(&self) -> Result<Vec<RawInterface>, String> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        if let Some(error) = self.fail.lock().expect("lock").clone() {
            return Err(error);
        }
        Ok(self.interfaces.lock().expect("lock").clone())
    }

    fn is_physical(&self, _name: &str) -> bool {
        true
    }

    fn ipv4_default_routes(&self) -> Result<Vec<RouteCandidate>, String> {
        Ok(Vec::new())
    }

    fn ipv6_default_routes(&self) -> Vec<RouteCandidate> {
        Vec::new()
    }
}

fn raw(name: &str, addrs: &[(&str, u8)]) -> RawInterface {
    RawInterface {
        name: name.to_string(),
        index: 1,
        mac: None,
        up: true,
        loopback: false,
        addrs: addrs
            .iter()
            .map(|(ip, prefix)| (ip.parse().unwrap(), *prefix))
            .collect(),
    }
}

fn setup() -> (Arc<FakeSource>, FlagPair, AddressCache) {
    let source = FakeSource::new(vec![
        raw(
            "eth0",
            &[
                ("192.168.1.10", 24),
                ("2001:db8::10", 64),
                ("fe80::10", 64),
            ],
        ),
        raw("lo", &[("127.0.0.1", 8), ("::1", 128)]),
        raw("eth1", &[("1.1.1.5", 24), ("2001:db8:1::5", 64)]),
    ]);
    let flags = FlagPair::new();
    let cache = AddressCache::with_refresh_interval(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        flags.flag(),
        Duration::ZERO,
    );
    (source, flags, cache)
}

#[test]
fn reports_assigned_and_global_addresses() {
    let (source, _, cache) = setup();

    let (ipv4, ipv6) = cache.get_assigned_addresses().expect("assigned");
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);
    assert_eq!(
        ipv4,
        vec![
            "192.168.1.10".parse::<std::net::Ipv4Addr>().unwrap(),
            "127.0.0.1".parse().unwrap(),
            "1.1.1.5".parse().unwrap()
        ]
    );
    assert_eq!(ipv6.len(), 4);

    let (global_v4, global_v6) = cache.get_assigned_global_addresses().expect("global");
    // 192.168.1.10 and 127.0.0.1 are not global.
    assert_eq!(
        global_v4,
        vec!["1.1.1.5".parse::<std::net::Ipv4Addr>().unwrap()]
    );
    assert_eq!(global_v6.len(), 2, "documentation v6 is global");
}

#[test]
fn is_my_ip_shortcuts_and_matching() {
    let (source, _, cache) = setup();

    // Localhost is answered without touching the OS.
    assert!(cache.is_my_ip("127.0.0.1".parse().unwrap()).expect("lo"));
    assert_eq!(source.calls.load(Ordering::SeqCst), 0);

    // Multicast is never ours.
    assert!(!cache.is_my_ip("224.0.0.1".parse().unwrap()).expect("mc"));
    assert_eq!(source.calls.load(Ordering::SeqCst), 0);

    // Exact match.
    assert!(cache
        .is_my_ip("192.168.1.10".parse().unwrap())
        .expect("own ip"));
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);

    // Another address in our network is not ours, and needs no refresh.
    assert!(!cache
        .is_my_ip("192.168.1.99".parse().unwrap())
        .expect("neighbor"));
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);

    // A completely unknown address triggers the final refresh.
    assert!(!cache
        .is_my_ip("8.8.8.8".parse().unwrap())
        .expect("unknown"));
    assert_eq!(source.calls.load(Ordering::SeqCst), 2);
}

#[test]
fn is_my_ip_reports_source_errors() {
    let (source, _, cache) = setup();
    *source.fail.lock().unwrap() = Some("boom".to_string());
    assert_eq!(
        cache.is_my_ip("8.8.8.8".parse().unwrap()).unwrap_err(),
        "failed to refresh interface addresses: boom"
    );
}

#[test]
fn finds_the_local_network() {
    let (_, _, cache) = setup();

    let network = cache
        .get_local_network("192.168.1.99".parse().unwrap())
        .expect("lookup")
        .expect("network");
    assert_eq!(network.network, "192.168.1.10".parse::<std::net::IpAddr>().unwrap());
    assert_eq!(network.prefix_len, 24);
    assert!(network.contains("192.168.1.200".parse().unwrap()));
    assert!(!network.contains("192.168.2.1".parse().unwrap()));

    let v6 = cache
        .get_local_network("2001:db8::99".parse().unwrap())
        .expect("lookup")
        .expect("v6 network");
    assert_eq!(v6.prefix_len, 64);

    assert!(cache
        .get_local_network("8.8.8.8".parse().unwrap())
        .expect("lookup")
        .is_none());
}

#[test]
fn refreshes_after_a_network_change() {
    let (source, flags, cache) = setup();
    assert!(cache
        .is_my_ip("192.168.1.10".parse().unwrap())
        .expect("first"));
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);

    // The address disappears and the network change is announced.
    source.interfaces.lock().unwrap().clear();
    flags.source().notify();
    assert!(!cache
        .is_my_ip("192.168.1.10".parse().unwrap())
        .expect("after change"));
    assert!(source.calls.load(Ordering::SeqCst) >= 2);
}
