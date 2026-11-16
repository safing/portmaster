//! Tests for the `pm-netenv`-backed split tunnel interface environment.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_netenv::{AddressCache, FlagPair, InterfaceCache, InterfaceSource, RawInterface, RouteCandidate};
use pm_splittun::{InterfaceEnv, SystemInterfaceEnv};

struct FakeSource {
    interfaces: Mutex<Vec<RawInterface>>,
    routes: Mutex<Vec<RouteCandidate>>,
}

impl FakeSource {
    fn new() -> Arc<FakeSource> {
        Arc::new(FakeSource {
            interfaces: Mutex::new(vec![RawInterface {
                name: "eth0".to_string(),
                index: 2,
                mac: Some("aa:bb:cc:dd:ee:ff".to_string()),
                up: true,
                loopback: false,
                addrs: vec![
                    ("192.168.1.10".parse().unwrap(), 24),
                    ("2001:db8::10".parse().unwrap(), 64),
                ],
            }]),
            routes: Mutex::new(vec![RouteCandidate {
                interface: "eth0".to_string(),
                metric: 10,
            }]),
        })
    }
}

impl InterfaceSource for FakeSource {
    fn interfaces(&self) -> Result<Vec<RawInterface>, String> {
        Ok(self.interfaces.lock().expect("lock").clone())
    }

    fn is_physical(&self, _name: &str) -> bool {
        true
    }

    fn ipv4_default_routes(&self) -> Result<Vec<RouteCandidate>, String> {
        Ok(self.routes.lock().expect("lock").clone())
    }

    fn ipv6_default_routes(&self) -> Vec<RouteCandidate> {
        Vec::new()
    }
}

fn env() -> SystemInterfaceEnv {
    let source = FakeSource::new();
    let flags = FlagPair::new();
    let interfaces = Arc::new(InterfaceCache::with_refresh_interval(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        flags.flag(),
        Duration::ZERO,
    ));
    let addresses = Arc::new(AddressCache::with_refresh_interval(
        source,
        flags.flag(),
        Duration::ZERO,
    ));
    SystemInterfaceEnv::new(interfaces, addresses)
}

#[test]
fn reports_ipv6_and_interfaces() {
    let env = env();
    assert!(env.ipv6_enabled());

    let info = env.get_interface("eth0").expect("interface");
    assert_eq!(info.name, "eth0");
    assert_eq!(info.ipv4, Some("192.168.1.10".parse().unwrap()));
    assert_eq!(info.ipv6, Some("2001:db8::10".parse().unwrap()));

    let by_ip = env
        .get_interface("192.168.1.10")
        .expect("interface by ip");
    assert_eq!(by_ip.name, "eth0");

    assert!(env
        .get_interface("does-not-exist")
        .unwrap_err()
        .contains("no interface found"));
}

#[test]
fn reports_physical_defaults() {
    let env = env();
    let best = env
        .best_physical_default_interfaces()
        .expect("physical defaults");
    assert_eq!(
        best.for_ipv4.as_ref().map(|info| info.name.as_str()),
        Some("eth0")
    );
    assert!(best.for_ipv6.is_none());
}

#[test]
fn no_addresses_means_no_ipv6() {
    let source = FakeSource::new();
    source.interfaces.lock().unwrap().clear();
    let flags = FlagPair::new();
    let interfaces = Arc::new(InterfaceCache::with_refresh_interval(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        flags.flag(),
        Duration::ZERO,
    ));
    let addresses = Arc::new(AddressCache::with_refresh_interval(
        source,
        flags.flag(),
        Duration::ZERO,
    ));
    let env = SystemInterfaceEnv::new(interfaces, addresses);
    assert!(!env.ipv6_enabled());
}
