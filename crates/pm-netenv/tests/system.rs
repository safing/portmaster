//! Smoke tests against the real operating system interfaces.
//!
//! These assertions are deliberately tolerant: CI hosts differ wildly, so they
//! only require the calls to succeed and to be self-consistent.

use pm_netenv::{network, SystemInterfaces, InterfaceSource};

#[test]
fn enumerates_the_real_interfaces() {
    let source = SystemInterfaces;
    let interfaces = source.interfaces().expect("enumerate interfaces");
    assert!(
        interfaces.iter().any(|interface| interface.name == "lo"),
        "loopback is always present: {interfaces:?}"
    );
}

#[test]
fn lookups_are_self_consistent() {
    let network = network();
    let interfaces = SystemInterfaces.interfaces().expect("interfaces");

    for interface in interfaces {
        for (ip, _prefix) in interface.addrs {
            // Skip addresses that the cache intentionally drops.
            if !pm_netenv::is_routable_unicast(pm_netenv::normalize_ip(ip)) {
                continue;
            }
            let info = network
                .interfaces
                .get_interface_by_ip(ip)
                .expect("interface by own address");
            assert_eq!(info.interface.name, interface.name, "address {ip}");
            assert!(network
                .addresses
                .is_my_ip(ip)
                .expect("is my ip"));
        }
    }
}

#[test]
fn physical_default_interfaces_do_not_error() {
    // A container without physical adapters simply returns empty fields.
    let result = network()
        .interfaces
        .best_physical_default_interfaces()
        .expect("physical defaults");
    if let Some(info) = result.for_ipv4 {
        assert!(info.ipv4.is_some());
    }
    if let Some(info) = result.for_ipv6 {
        assert!(info.ipv6.is_some());
    }
}
