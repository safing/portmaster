//! Tests for the nameserver and gateway caches (Go `netenv/environment_linux.go`).

use std::net::{IpAddr, Ipv4Addr};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use pm_netenv::dbus::ConnectivityState;
use pm_netenv::{
    add_nameservers, notify_network_change, parse_ipv4_routes, parse_ipv6_routes, parse_resolvconf,
    EnvironmentCache, EnvironmentSource, Nameserver,
};

struct FakeEnvironment {
    nameservers: Vec<Nameserver>,
    gateways: Vec<pm_netenv::GatewayInfo>,
    connectivity: ConnectivityState,
    nameserver_calls: AtomicUsize,
    gateway_calls: AtomicUsize,
}

impl EnvironmentSource for FakeEnvironment {
    fn nameservers(&self) -> Result<Vec<Nameserver>, String> {
        self.nameserver_calls.fetch_add(1, Ordering::SeqCst);
        Ok(self.nameservers.clone())
    }

    fn gateways(&self) -> Result<Vec<pm_netenv::GatewayInfo>, String> {
        self.gateway_calls.fetch_add(1, Ordering::SeqCst);
        Ok(self.gateways.clone())
    }

    fn connectivity_state(&self) -> Result<ConnectivityState, String> {
        Ok(self.connectivity)
    }
}

#[test]
fn resolv_conf_parses_search_domains_and_nameservers() {
    let data = "# comment\nnameserver 192.0.2.1\nnameserver 2001:db8::1\nsearch example.com invalid-domain_ bad\nnameserver not-an-ip\n";
    let servers = parse_resolvconf(data).expect("parse resolv.conf");
    assert_eq!(servers.len(), 2);
    assert_eq!(servers[0].ip, "192.0.2.1".parse::<IpAddr>().unwrap());
    assert_eq!(servers[1].ip, "2001:db8::1".parse::<IpAddr>().unwrap());
    assert_eq!(servers[0].search, vec!["example.com"]);
    assert_eq!(servers[1].search, vec!["example.com"]);
}

#[test]
fn nameservers_are_deduplicated_by_ip() {
    let first = Nameserver {
        ip: "192.0.2.1".parse().unwrap(),
        search: vec!["a.example".to_string()],
    };
    let duplicate = Nameserver {
        ip: "192.0.2.1".parse().unwrap(),
        search: vec!["b.example".to_string()],
    };
    let second = Nameserver {
        ip: "192.0.2.2".parse().unwrap(),
        search: vec![],
    };
    let merged = add_nameservers(vec![first.clone()], vec![duplicate, second.clone()]);
    assert_eq!(merged, vec![first, second]);
}

#[test]
fn ipv4_routes_are_decoded_little_endian() {
    // Gateway 192.168.1.1 is 0101a8c0 in /proc/net/route; netmask 255.255.255.0
    // is 00ffffff.
    let data = "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\neth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00FFFFFF\ntun0\t00000000\t00000000\t0001\t0\t0\t0\t00000000\n";
    let gateways = parse_ipv4_routes(data).expect("parse routes");
    assert_eq!(
        gateways.len(),
        2,
        "Go keeps zero-gateway default routes too"
    );
    assert_eq!(gateways[0].ip, "192.168.1.1".parse::<IpAddr>().unwrap());
    assert_eq!(gateways[0].interface, "eth0");
    assert_eq!(gateways[0].mask, "255.255.255.0");
    assert_eq!(gateways[1].ip, "0.0.0.0".parse::<IpAddr>().unwrap());
}

#[test]
fn ipv6_routes_ignore_link_local_entries() {
    let zero = "00000000000000000000000000000000";
    let data = format!(
        "{zero} 00 {zero} 00 20010db8000000000000000000000001 00000100 0 0 00000000 eth0\n\
         {zero} 00 {zero} 00 fe800000000000000000000000000001 00000100 0 0 00000000 eth0\n"
    );
    let gateways = parse_ipv6_routes(&data);
    assert_eq!(gateways.len(), 2, "Go does not filter link-local gateways");
    assert_eq!(gateways[0].ip, "2001:db8::1".parse::<IpAddr>().unwrap());
    assert_eq!(gateways[0].mask, "/0");
    assert_eq!(gateways[0].interface, "eth0");
}

#[test]
fn caches_refresh_only_after_a_network_change() {
    let source = Arc::new(FakeEnvironment {
        nameservers: vec![Nameserver {
            ip: IpAddr::V4(Ipv4Addr::new(192, 0, 2, 1)),
            search: vec![],
        }],
        gateways: vec![],
        connectivity: ConnectivityState::Full,
        nameserver_calls: AtomicUsize::new(0),
        gateway_calls: AtomicUsize::new(0),
    });
    let cache = EnvironmentCache::new(source.clone());

    // The first lookup always loads, like Go's initially-set flag.
    assert_eq!(cache.nameservers().len(), 1);
    assert_eq!(cache.nameservers().len(), 1);
    assert_eq!(source.nameserver_calls.load(Ordering::SeqCst), 1);

    cache.gateways_info();
    cache.gateways_info();
    assert_eq!(source.gateway_calls.load(Ordering::SeqCst), 1);

    // A global network change notification invalidates both caches.
    notify_network_change();
    assert_eq!(cache.nameservers().len(), 1);
    assert_eq!(cache.gateways_info().len(), 0);
    assert_eq!(source.nameserver_calls.load(Ordering::SeqCst), 2);
    assert_eq!(source.gateway_calls.load(Ordering::SeqCst), 2);
}

#[test]
fn connectivity_state_maps_to_online_status() {
    assert_eq!(
        ConnectivityState::from_network_manager(0).online_status(),
        pm_netenv::OnlineStatus::Unknown
    );
    assert_eq!(
        ConnectivityState::from_network_manager(1).online_status(),
        pm_netenv::OnlineStatus::Offline
    );
    assert_eq!(
        ConnectivityState::from_network_manager(2).online_status(),
        pm_netenv::OnlineStatus::Portal
    );
    assert_eq!(
        ConnectivityState::from_network_manager(3).online_status(),
        pm_netenv::OnlineStatus::Limited
    );
    assert_eq!(
        ConnectivityState::from_network_manager(4).online_status(),
        pm_netenv::OnlineStatus::Online
    );
}
