//! Tests for the firewall adapter of the split tunnel module.

use std::sync::Arc;

use pm_endpoints::EpResult;
use pm_firewall::SplitTunnelEnv;
use pm_intel::{Entity, FilterLists, StaticLocations};
use pm_network::Connection;
use pm_process::Process;
use pm_splittun::{
    BestPhysicalDefaultInterfaces, InterfaceEnv, InterfaceInfo, Proxies, RequestQueue,
    SplitTunnelEnvAdapter, SplitTunnelProfiles,
};

struct FakeInterfaces;

impl InterfaceEnv for FakeInterfaces {
    fn ipv6_enabled(&self) -> bool {
        false
    }

    fn get_interface(&self, _identifier: &str) -> Result<InterfaceInfo, String> {
        Ok(InterfaceInfo {
            name: "eth0".to_string(),
            ipv4: Some("192.168.1.10".parse().unwrap()),
            ipv6: None,
        })
    }

    fn best_physical_default_interfaces(&self) -> Result<BestPhysicalDefaultInterfaces, String> {
        Ok(BestPhysicalDefaultInterfaces {
            for_ipv4: Some(InterfaceInfo {
                name: "eth0".to_string(),
                ipv4: Some("192.168.1.10".parse().unwrap()),
                ipv6: None,
            }),
            for_ipv6: None,
        })
    }
}

struct FakeProfiles {
    revision: Option<u64>,
    use_split_tun: bool,
    proxy: String,
    interface: String,
    policy: EpResult,
}

impl SplitTunnelProfiles for FakeProfiles {
    fn profile_revision(&self, _process: &Process) -> Option<u64> {
        self.revision
    }

    fn use_split_tun(&self, _process: &Process) -> bool {
        self.use_split_tun
    }

    fn split_tun_proxy(&self, _process: &Process) -> String {
        self.proxy.clone()
    }

    fn split_tun_interface(&self, _process: &Process) -> String {
        self.interface.clone()
    }

    fn match_split_tun_usage_policy(&self, _process: &Process, _entity: &mut Entity) -> EpResult {
        self.policy
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

fn connection() -> Connection {
    let mut entity = Entity::new(lists(), Arc::new(StaticLocations::new()));
    entity.ip = Some("1.1.1.1".parse().unwrap());
    entity.port = 443;
    entity.init(443);

    Connection {
        ip_version: 4,
        ip_protocol: 6,
        local_ip: Some("192.168.1.5".parse().unwrap()),
        local_port: 41000,
        process: Some(Arc::new(Process::empty("1-0"))),
        entity: Some(entity),
        ..Connection::default()
    }
}

fn adapter(profiles: FakeProfiles) -> SplitTunnelEnvAdapter {
    SplitTunnelEnvAdapter::new(
        1234,
        Arc::new(RequestQueue::new()),
        Arc::new(Proxies::new()),
        Arc::new(FakeInterfaces),
        Arc::new(profiles),
    )
}

fn profiles() -> FakeProfiles {
    FakeProfiles {
        revision: Some(7),
        use_split_tun: true,
        proxy: "socks5://127.0.0.1:1080".to_string(),
        interface: "eth0".to_string(),
        policy: EpResult::NoMatch,
    }
}

#[test]
fn exposes_the_module_state() {
    let env = adapter(profiles());
    let conn = connection();

    assert_eq!(env.own_pid(), 1234);
    assert!(!env.is_ready(), "proxies are not started");
    assert!(!env.is_proxied_connection_info(&conn));
    assert_eq!(env.profile_revision(&conn), Some(7));
    assert!(env.use_split_tun(&conn));
    assert_eq!(env.split_tun_proxy(&conn), "socks5://127.0.0.1:1080");
    assert_eq!(env.split_tun_interface(&conn), "eth0");
    let mut policy_conn = connection();
    assert_eq!(
        env.match_split_tun_usage_policy(&mut policy_conn),
        EpResult::NoMatch
    );
}

#[test]
fn missing_process_uses_safe_defaults() {
    let env = adapter(profiles());
    let conn = Connection::default();

    assert_eq!(env.profile_revision(&conn), None);
    assert!(!env.use_split_tun(&conn));
    assert_eq!(env.split_tun_proxy(&conn), "");
    assert_eq!(env.split_tun_interface(&conn), "");
    assert_eq!(
        env.match_split_tun_usage_policy(&mut Connection::default()),
        EpResult::NoMatch
    );
}

#[test]
fn requests_are_forwarded_to_the_queue() {
    let env = adapter(FakeProfiles {
        revision: Some(1),
        use_split_tun: true,
        proxy: String::new(),
        interface: "eth0".to_string(),
        policy: EpResult::NoMatch,
    });

    let conn = connection();
    let context = env
        .await_request(&conn, "eth0", "")
        .expect("await request");
    assert_eq!(context.interface, "eth0");
    assert_eq!(env.queue().len(), 1);

    let request = env.queue().consume("192.168.1.5:41000").expect("consume");
    assert_eq!(request.dest_port, 443);
}
