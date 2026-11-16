//! Tests for the split tunnel request queue (Go `splittun/requests.go`).

use std::net::IpAddr;
use std::sync::Arc;

use pm_intel::{Entity, FilterLists, StaticLocations};
use pm_network::{Connection, JsonIp};
use pm_splittun::{
    BestPhysicalDefaultInterfaces, InterfaceEnv, InterfaceInfo, RequestQueue, PENDING_REQUEST_TTL,
    SPLIT_TUN_PORT,
};

struct FakeEnv {
    ipv6: bool,
    ipv4_iface: InterfaceInfo,
    ipv6_iface: InterfaceInfo,
}

impl Default for FakeEnv {
    fn default() -> Self {
        FakeEnv {
            ipv6: true,
            ipv4_iface: InterfaceInfo {
                name: "eth0".to_string(),
                ipv4: Some("192.168.1.10".parse().unwrap()),
                ipv6: Some("2001:db8::10".parse().unwrap()),
            },
            ipv6_iface: InterfaceInfo {
                name: "eth0".to_string(),
                ipv4: Some("192.168.1.10".parse().unwrap()),
                ipv6: Some("2001:db8::10".parse().unwrap()),
            },
        }
    }
}

impl InterfaceEnv for FakeEnv {
    fn ipv6_enabled(&self) -> bool {
        self.ipv6
    }

    fn get_interface(&self, _identifier: &str) -> Result<InterfaceInfo, String> {
        Ok(self.ipv4_iface.clone())
    }

    fn best_physical_default_interfaces(&self) -> Result<BestPhysicalDefaultInterfaces, String> {
        Ok(BestPhysicalDefaultInterfaces {
            for_ipv4: Some(self.ipv4_iface.clone()),
            for_ipv6: if self.ipv6 {
                Some(self.ipv6_iface.clone())
            } else {
                None
            },
        })
    }
}

fn lists() -> Arc<FilterLists> {
    let databases = Arc::new(pm_database::Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .expect("register");
    databases
        .inject("cache", Box::new(pm_database::MemoryStorage::new()))
        .expect("inject");
    Arc::new(FilterLists::with_capacity(
        pm_intel::cache_interface(&databases),
        10,
        10,
        1,
        1,
        1,
    ))
}

fn connection(ip_version: u8, local_port: u16) -> Connection {
    let mut entity = Entity::new(lists(), Arc::new(StaticLocations::new()));
    entity.ip = Some(if ip_version == 6 {
        "2001:db8::1".parse().unwrap()
    } else {
        "1.1.1.1".parse().unwrap()
    });
    entity.domain = "example.com".to_string();
    entity.port = 443;
    entity.init(443);

    Connection {
        ip_version,
        local_ip: Some(if ip_version == 6 {
            "2001:db8::2".parse().unwrap()
        } else {
            "192.168.1.5".parse().unwrap()
        }),
        local_port,
        entity: Some(entity),
        ..Connection::default()
    }
}

#[test]
fn registers_and_consumes_a_request() {
    let queue = RequestQueue::new();
    let conn = connection(4, 40000);
    let env = FakeEnv::default();

    let context = queue
        .await_request(&conn, "eth0", "", &env)
        .expect("await request");
    assert_eq!(context.interface, "eth0");
    assert_eq!(context.ip, JsonIp(Some("192.168.1.10".parse().unwrap())));
    assert_eq!(context.proxy, "");
    assert_eq!(queue.len(), 1);

    let key = "192.168.1.5:40000".to_string();
    let request = queue.consume(&key).expect("consume");
    assert_eq!(request.dest_ip, "1.1.1.1".parse::<IpAddr>().unwrap());
    assert_eq!(request.dest_port, 443);
    assert_eq!(request.domain, "example.com");
    assert!(queue.is_empty());

    let error = queue.consume(&key).expect_err("consumed once");
    assert_eq!(error, format!("no pending request for {key}"));
}

#[test]
fn duplicate_pending_requests_are_rejected() {
    let queue = RequestQueue::new();
    let env = FakeEnv::default();
    queue
        .await_request(&connection(4, 40001), "eth0", "", &env)
        .expect("first");

    let error = queue
        .await_request(&connection(4, 40001), "eth0", "", &env)
        .expect_err("duplicate");
    assert_eq!(
        error,
        "a pending request for 192.168.1.5:40001 already exists"
    );
}

#[test]
fn empty_interface_uses_the_best_physical_default() {
    let queue = RequestQueue::new();
    let env = FakeEnv::default();

    let v4 = queue
        .await_request(&connection(4, 40002), "", "", &env)
        .expect("v4");
    assert_eq!(v4.interface, "eth0");
    assert_eq!(v4.ip, JsonIp(Some("192.168.1.10".parse().unwrap())));

    let v6 = queue
        .await_request(&connection(6, 40003), "", "", &env)
        .expect("v6");
    assert_eq!(v6.interface, "eth0");
    assert_eq!(v6.ip, JsonIp(Some("2001:db8::10".parse().unwrap())));
}

#[test]
fn missing_default_interface_is_an_error() {
    let queue = RequestQueue::new();
    let env = FakeEnv {
        ipv6: false,
        ..FakeEnv::default()
    };

    let error = queue
        .await_request(&connection(6, 40004), "", "", &env)
        .expect_err("no v6 interface");
    assert_eq!(
        error,
        "no suitable default physical interface found for IPv6"
    );
}

#[test]
fn named_interface_picks_the_address_of_the_connection_version() {
    let queue = RequestQueue::new();
    let env = FakeEnv::default();

    let v6 = queue
        .await_request(&connection(6, 40005), "eth0", "", &env)
        .expect("v6");
    assert_eq!(v6.ip, JsonIp(Some("2001:db8::10".parse().unwrap())));

    // With an upstream proxy, the other address version is kept as a fallback.
    let conn = connection(4, 40006);
    queue
        .await_request(&conn, "eth0", "socks5://127.0.0.1:1080", &env)
        .expect("upstream");
    let request = queue.consume("192.168.1.5:40006").expect("consume");
    assert_eq!(
        request.binding.alt_ip,
        Some("2001:db8::10".parse().unwrap())
    );
}

#[test]
fn upstream_proxy_is_parsed_and_redacted() {
    let queue = RequestQueue::new();
    let env = FakeEnv::default();
    let conn = connection(4, 40007);

    let context = queue
        .await_request(&conn, "", "socks5://alice:secret@proxy.lan:1080", &env)
        .expect("upstream");
    assert_eq!(context.proxy, "socks5://proxy.lan:1080");
    // No local binding for an empty interface and an upstream proxy.
    assert_eq!(context.ip, JsonIp(None));

    let request = queue.consume("192.168.1.5:40007").expect("consume");
    let upstream = request.binding.upstream.expect("upstream");
    assert_eq!(upstream.host, "proxy.lan");
    assert_eq!(upstream.port, 1080);
    assert_eq!(upstream.username.as_deref(), Some("alice"));
    assert_eq!(upstream.target_host_override.as_deref(), Some("example.com"));
}

#[test]
fn malformed_upstream_proxy_uses_the_go_message() {
    let queue = RequestQueue::new();
    let env = FakeEnv::default();

    let error = queue
        .await_request(&connection(4, 40008), "", "socks5://proxy.lan", &env)
        .expect_err("missing port");
    assert_eq!(error, "proxy URL is missing a valid port");

    let error = queue
        .await_request(&connection(4, 40009), "", "ftp://proxy.lan:21", &env)
        .expect_err("unsupported scheme");
    assert!(error.starts_with("unsupported proxy scheme \"ftp\""), "{error}");
}

#[test]
fn expired_requests_are_swept() {
    let queue = RequestQueue::new();
    let env = FakeEnv::default();
    queue
        .await_request(&connection(4, 40010), "eth0", "", &env)
        .expect("await");
    assert_eq!(queue.len(), 1);

    // Jump past the TTL instead of sleeping for it.
    let later = std::time::Instant::now() + PENDING_REQUEST_TTL + std::time::Duration::from_secs(1);
    queue.sweep_at(later);
    assert!(queue.is_empty());

    // Go reports an expiration when a request is consumed after the TTL.
    queue
        .await_request(&connection(4, 40011), "eth0", "", &env)
        .expect("await");
    let error = queue
        .consume_at("192.168.1.5:40011", later)
        .expect_err("expired");
    assert_eq!(
        error,
        "pending request for 192.168.1.5:40011 has expired"
    );
}

#[test]
fn constants_match_go() {
    assert_eq!(SPLIT_TUN_PORT, 719);
    assert_eq!(PENDING_REQUEST_TTL.as_secs(), 30);
}

#[test]
fn missing_local_ip_is_an_error() {
    let queue = RequestQueue::new();
    let env = FakeEnv::default();
    let mut conn = connection(4, 40012);
    conn.local_ip = None;

    let error = queue
        .await_request(&conn, "eth0", "", &env)
        .expect_err("no local ip");
    assert_eq!(error, "connection has no local IP");
}
