//! Tests for the interface cache (Go `netenv/interfaces.go`).

use std::collections::HashSet;
use std::net::IpAddr;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_netenv::{
    canonical_mac, network_contains, FlagPair, InterfaceCache, InterfaceSource, RawInterface,
    RouteCandidate,
};

#[derive(Default)]
struct FakeSource {
    interfaces: Mutex<Vec<RawInterface>>,
    physical: Mutex<HashSet<String>>,
    ipv4_routes: Mutex<Vec<RouteCandidate>>,
    ipv6_routes: Mutex<Vec<RouteCandidate>>,
    calls: AtomicUsize,
    fail: Mutex<Option<String>>,
}

impl InterfaceSource for FakeSource {
    fn interfaces(&self) -> Result<Vec<RawInterface>, String> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        if let Some(error) = self.fail.lock().expect("lock").clone() {
            return Err(error);
        }
        Ok(self.interfaces.lock().expect("lock").clone())
    }

    fn is_physical(&self, name: &str) -> bool {
        self.physical.lock().expect("lock").contains(name)
    }

    fn ipv4_default_routes(&self) -> Result<Vec<RouteCandidate>, String> {
        Ok(self.ipv4_routes.lock().expect("lock").clone())
    }

    fn ipv6_default_routes(&self) -> Vec<RouteCandidate> {
        self.ipv6_routes.lock().expect("lock").clone()
    }
}

fn raw(
    name: &str,
    mac: Option<&str>,
    up: bool,
    loopback: bool,
    addrs: &[(&str, u8)],
) -> RawInterface {
    RawInterface {
        name: name.to_string(),
        index: 1,
        mac: mac.map(str::to_string),
        up,
        loopback,
        addrs: addrs
            .iter()
            .map(|(ip, prefix)| (ip.parse().unwrap(), *prefix))
            .collect(),
    }
}

fn setup() -> (Arc<FakeSource>, FlagPair, InterfaceCache) {
    let source = Arc::new(FakeSource::default());
    {
        let mut interfaces = source.interfaces.lock().unwrap();
        interfaces.extend([
            raw(
                "eth0",
                Some("aa:bb:cc:dd:ee:ff"),
                true,
                false,
                &[
                    ("192.168.1.10", 24),
                    ("2001:db8::10", 64),
                    ("fe80::10", 64),
                ],
            ),
            raw(
                "lo",
                Some("00:00:00:00:00:00"),
                true,
                true,
                &[("127.0.0.1", 8)],
            ),
            raw(
                "eth1",
                Some("aa:bb:cc:dd:ee:01"),
                false,
                false,
                &[("10.0.0.2", 8)],
            ),
            raw(
                "eth2",
                None,
                true,
                false,
                &[("169.254.1.1", 16)],
            ),
        ]);
    }
    let flags = FlagPair::new();
    let cache = InterfaceCache::with_refresh_interval(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        flags.flag(),
        Duration::ZERO,
    );
    (source, flags, cache)
}

#[test]
fn filters_unusable_interfaces_and_addresses() {
    let (_, _, cache) = setup();

    let eth0 = cache.get_interface_by_name("eth0").expect("eth0");
    assert_eq!(eth0.ipv4, Some("192.168.1.10".parse().unwrap()));
    assert_eq!(eth0.ipv6, Some("2001:db8::10".parse().unwrap()));
    assert_eq!(eth0.interface.mac, "aa:bb:cc:dd:ee:ff");
    // Link-local addresses are not routable and are dropped.
    assert_eq!(eth0.interface.addrs.len(), 2);

    assert!(cache.get_interface_by_name("lo").is_err());
    assert!(cache.get_interface_by_name("eth1").is_err());
    assert!(cache.get_interface_by_name("eth2").is_err());
}

#[test]
fn looks_up_by_ip_mac_and_name() {
    let (_, _, cache) = setup();

    let by_ip = cache
        .get_interface_by_ip("192.168.1.10".parse().unwrap())
        .expect("by ip");
    assert_eq!(by_ip.interface.name, "eth0");
    assert_eq!(by_ip.ipv4, Some("192.168.1.10".parse().unwrap()));
    // The other family is filled from the interface.
    assert_eq!(by_ip.ipv6, Some("2001:db8::10".parse().unwrap()));

    // IPv4-mapped IPv6 is normalized.
    let mapped = cache
        .get_interface("::ffff:192.168.1.10")
        .expect("mapped ip");
    assert_eq!(mapped.interface.name, "eth0");

    // MAC lookup is case-insensitive and accepts separators.
    for mac in ["aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF", "aa-bb-cc-dd-ee-ff"] {
        let info = cache.get_interface_by_mac(mac).expect(mac);
        assert_eq!(info.interface.name, "eth0");
    }

    let by_name = cache.get_interface("eth0").expect("by name");
    assert_eq!(by_name.ipv4, Some("192.168.1.10".parse().unwrap()));

    assert_eq!(
        cache.get_interface("nope").unwrap_err(),
        "no interface found \"nope\""
    );
    assert_eq!(
        cache.get_interface_by_ip("10.9.9.9".parse().unwrap())
            .unwrap_err(),
        "no interface found with IP 10.9.9.9"
    );
    assert_eq!(
        cache.get_interface_by_mac("aa:aa:aa:aa:aa:aa").unwrap_err(),
        "no interface found with MAC aa:aa:aa:aa:aa:aa"
    );
    assert_eq!(
        cache.get_interface_by_name("eth9").unwrap_err(),
        "no interface found with name \"eth9\""
    );
}

#[test]
fn picks_the_best_physical_default_interfaces() {
    let (source, flags, cache) = setup();
    source.physical.lock().unwrap().extend(["eth0".to_string()]);
    {
        let mut interfaces = source.interfaces.lock().unwrap();
        interfaces.push(raw(
            "wlan0",
            Some("aa:bb:cc:dd:ee:02"),
            true,
            false,
            &[("10.1.0.5", 24), ("2001:db8:1::5", 64)],
        ));
    }
    source.ipv4_routes.lock().unwrap().extend([
        RouteCandidate {
            interface: "eth0".to_string(),
            metric: 100,
        },
        RouteCandidate {
            interface: "wlan0".to_string(),
            metric: 50,
        },
    ]);
    source.ipv6_routes.lock().unwrap().extend([
        RouteCandidate {
            interface: "eth0".to_string(),
            metric: 100,
        },
        RouteCandidate {
            interface: "wlan0".to_string(),
            metric: 50,
        },
    ]);

    let best = cache.best_physical_default_interfaces().expect("best");
    // Only eth0 is physical, even though wlan0 has the lower metric.
    assert_eq!(
        best.for_ipv4.as_ref().map(|info| info.interface.name.as_str()),
        Some("eth0")
    );
    assert_eq!(
        best.for_ipv4.as_ref().and_then(|info| info.ipv4),
        Some("192.168.1.10".parse().unwrap())
    );
    assert_eq!(
        best.for_ipv6.as_ref().map(|info| info.interface.name.as_str()),
        Some("eth0")
    );

    // The network changed: eth0 is no longer physical, wlan0 is.
    source.physical.lock().unwrap().remove("eth0");
    source.physical.lock().unwrap().insert("wlan0".to_string());
    flags.source().notify();
    let best = cache.best_physical_default_interfaces().expect("best");
    assert_eq!(
        best.for_ipv4.as_ref().map(|info| info.interface.name.as_str()),
        Some("wlan0")
    );
}

#[test]
fn physical_candidates_need_a_routable_address() {
    let (source, _, cache) = setup();
    source.physical.lock().unwrap().insert("eth2".to_string());
    source.ipv4_routes.lock().unwrap().push(RouteCandidate {
        interface: "eth2".to_string(),
        metric: 1,
    });

    let best = cache.best_physical_default_interfaces().expect("best");
    assert!(best.for_ipv4.is_none(), "eth2 has no routable address");
    assert!(best.for_ipv6.is_none());
}

#[test]
fn refreshes_only_when_the_network_changed() {
    let (source, flags, cache) = setup();
    cache.get_interface("eth0").expect("first");
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);
    cache.get_interface("eth0").expect("cached");
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);

    flags.source().notify();
    cache.get_interface("eth0").expect("refreshed");
    assert_eq!(source.calls.load(Ordering::SeqCst), 2);
}

#[test]
fn refresh_is_throttled() {
    let source = Arc::new(FakeSource::default());
    source
        .interfaces
        .lock()
        .unwrap()
        .push(raw("eth0", None, true, false, &[("192.168.1.10", 24)]));
    let flags = FlagPair::new();
    let cache = InterfaceCache::with_refresh_interval(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        flags.flag(),
        Duration::from_secs(3600),
    );

    cache.get_interface("eth0").expect("first");
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);
    flags.source().notify();
    cache.get_interface("eth0").expect("throttled");
    assert_eq!(source.calls.load(Ordering::SeqCst), 1);
}

#[test]
fn enumeration_errors_are_reported() {
    let source = Arc::new(FakeSource::default());
    *source.fail.lock().unwrap() = Some("boom".to_string());
    let flags = FlagPair::new();
    let cache = InterfaceCache::with_refresh_interval(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        flags.flag(),
        Duration::ZERO,
    );

    assert_eq!(
        cache.get_interface("eth0").unwrap_err(),
        "failed to enumerate network interfaces: boom"
    );
}

#[test]
fn helpers_match_go() {
    assert_eq!(
        canonical_mac("AA:BB:CC:DD:EE:FF"),
        Some("aa:bb:cc:dd:ee:ff".to_string())
    );
    assert_eq!(canonical_mac("aa:bb:cc:dd:ee"), None);
    assert_eq!(canonical_mac("nonsense"), None);

    assert!(network_contains(
        "192.168.1.10".parse().unwrap(),
        24,
        "192.168.1.99".parse().unwrap()
    ));
    assert!(!network_contains(
        "192.168.1.10".parse::<IpAddr>().unwrap(),
        24,
        "192.168.2.1".parse().unwrap()
    ));
    assert!(network_contains(
        "2001:db8::10".parse().unwrap(),
        64,
        "2001:db8::99".parse().unwrap()
    ));
    assert!(!network_contains(
        "2001:db8::10".parse::<IpAddr>().unwrap(),
        64,
        "2001:db9::1".parse().unwrap()
    ));
    // Mixed families never match.
    assert!(!network_contains(
        "192.168.1.10".parse().unwrap(),
        24,
        "2001:db8::1".parse().unwrap()
    ));
}
