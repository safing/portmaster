//! Tests for the split tunneling verdict (Go `firewall/split-tunnel.go`).

use std::sync::{Arc, Mutex};

use pm_endpoints::EpResult;
use pm_firewall::{
    check_split_tunneling, is_own_split_tunnel_proxy_connection, request_split_tunneling,
    SplitTunnelEnv,
};
use pm_intel::{Entity, FilterLists, StaticLocations};
use pm_netstate::IpScope;
use pm_network::{protocol, Connection, SplitTunContext, Verdict};

fn lists() -> Arc<FilterLists> {
    // A database-free filter list with the minimum capacities.
    let databases = Arc::new(pm_database::Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .expect("register cache");
    databases
        .inject("cache", Box::new(pm_database::MemoryStorage::new()))
        .expect("inject storage");
    Arc::new(FilterLists::with_capacity(
        pm_intel::cache_interface(&databases),
        10,
        10,
        1,
        1,
        1,
    ))
}

struct FakeEnv {
    own_pid: i32,
    ready: bool,
    revision: Option<u64>,
    use_split_tun: bool,
    proxy: String,
    interface: String,
    policy: EpResult,
    proxied: bool,
    requests: Mutex<Vec<(String, String)>>,
}

impl Default for FakeEnv {
    fn default() -> Self {
        FakeEnv {
            own_pid: 100,
            ready: true,
            revision: Some(7),
            use_split_tun: true,
            proxy: String::new(),
            interface: "portmaster0".to_string(),
            policy: EpResult::NoMatch,
            proxied: false,
            requests: Mutex::new(Vec::new()),
        }
    }
}

impl SplitTunnelEnv for FakeEnv {
    fn own_pid(&self) -> i32 {
        self.own_pid
    }

    fn is_ready(&self) -> bool {
        self.ready
    }

    fn is_proxied_connection_info(&self, _conn: &Connection) -> bool {
        self.proxied
    }

    fn profile_revision(&self, _conn: &Connection) -> Option<u64> {
        self.revision
    }

    fn use_split_tun(&self, _conn: &Connection) -> bool {
        self.use_split_tun
    }

    fn split_tun_proxy(&self, _conn: &Connection) -> String {
        self.proxy.clone()
    }

    fn split_tun_interface(&self, _conn: &Connection) -> String {
        self.interface.clone()
    }

    fn match_split_tun_usage_policy(&self, _conn: &mut Connection) -> EpResult {
        self.policy
    }

    fn await_request(
        &self,
        conn: &Connection,
        interface: &str,
        upstream_proxy: &str,
    ) -> Result<SplitTunContext, String> {
        assert!(conn.process.is_some(), "the connection has a process");
        self.requests
            .lock()
            .expect("lock")
            .push((interface.to_string(), upstream_proxy.to_string()));
        Ok(SplitTunContext {
            interface: interface.to_string(),
            ip: Default::default(),
            proxy: upstream_proxy.to_string(),
        })
    }
}

fn connection() -> Connection {
    let mut entity = Entity::new(lists(), Arc::new(StaticLocations::new()));
    entity.ip = Some("1.1.1.1".parse().unwrap());
    entity.protocol = protocol::TCP;
    entity.ip_scope = IpScope::Global;
    entity.init(443);

    Connection {
        ip_protocol: protocol::TCP,
        pid: 200,
        entity: Some(entity),
        process: Some(Arc::new(pm_process::Process::empty("200-0"))),
        verdict: Verdict::Accept,
        ..Connection::default()
    }
}

#[test]
fn accepted_tcp_connection_is_rerouted() {
    let mut conn = connection();
    let env = FakeEnv::default();

    check_split_tunneling(&mut conn, &env);

    assert_eq!(conn.verdict, Verdict::RerouteToSplitTun);
    assert!(conn.save_when_finished);
    assert_eq!(conn.profile_revision_counter, 7);
}

#[test]
fn missing_profile_fails_the_connection() {
    let mut conn = connection();
    let env = FakeEnv {
        revision: None,
        ..FakeEnv::default()
    };

    check_split_tunneling(&mut conn, &env);

    assert_eq!(conn.verdict, Verdict::Failed);
    assert_eq!(conn.reason.msg, "no profile set");
    assert!(!conn.save_when_finished);
}

#[test]
fn split_tunnel_must_be_enabled_for_the_app() {
    let mut conn = connection();
    let env = FakeEnv {
        use_split_tun: false,
        ..FakeEnv::default()
    };

    check_split_tunneling(&mut conn, &env);

    assert_eq!(conn.verdict, Verdict::Accept);
    // The revision is still refreshed before the app check.
    assert_eq!(conn.profile_revision_counter, 7);
    assert!(conn.save_when_finished);
}

#[test]
fn matching_errors_fail_and_denials_keep_the_verdict() {
    let mut conn = connection();
    let env = FakeEnv {
        policy: EpResult::MatchError,
        ..FakeEnv::default()
    };
    check_split_tunneling(&mut conn, &env);
    assert_eq!(conn.verdict, Verdict::Failed);
    assert_eq!(conn.reason.msg, "failed to check Split Tunnel rules");

    let mut conn = connection();
    let env = FakeEnv {
        policy: EpResult::Denied,
        ..FakeEnv::default()
    };
    check_split_tunneling(&mut conn, &env);
    assert_eq!(conn.verdict, Verdict::Accept);
}

#[test]
fn udp_is_blocked_when_an_upstream_proxy_is_set() {
    let mut conn = connection();
    conn.ip_protocol = protocol::UDP;
    let env = FakeEnv {
        proxy: "socks5://127.0.0.1:1080".to_string(),
        ..FakeEnv::default()
    };

    check_split_tunneling(&mut conn, &env);

    assert_eq!(conn.verdict, Verdict::Block);
    assert_eq!(
        conn.reason.msg,
        "UDP cannot be relayed through the Split Tunnel proxy"
    );
}

#[test]
fn udp_without_proxy_is_rerouted() {
    let mut conn = connection();
    conn.ip_protocol = protocol::UDP;
    let env = FakeEnv::default();

    check_split_tunneling(&mut conn, &env);

    assert_eq!(conn.verdict, Verdict::RerouteToSplitTun);
}

#[test]
fn skipped_connections_keep_their_verdict() {
    // Inbound.
    let mut conn = connection();
    conn.inbound = true;
    check_split_tunneling(&mut conn, &FakeEnv::default());
    assert_eq!(conn.verdict, Verdict::Accept);

    // Not accepted yet.
    let mut conn = connection();
    conn.verdict = Verdict::Block;
    check_split_tunneling(&mut conn, &FakeEnv::default());
    assert_eq!(conn.verdict, Verdict::Block);

    // Unsupported protocol.
    let mut conn = connection();
    conn.ip_protocol = protocol::ICMP;
    check_split_tunneling(&mut conn, &FakeEnv::default());
    assert_eq!(conn.verdict, Verdict::Accept);

    // Own connection.
    let mut conn = connection();
    conn.pid = 100;
    check_split_tunneling(&mut conn, &FakeEnv::default());
    assert_eq!(conn.verdict, Verdict::Accept);

    // Proxy not ready.
    let mut conn = connection();
    let env = FakeEnv {
        ready: false,
        ..FakeEnv::default()
    };
    check_split_tunneling(&mut conn, &env);
    assert_eq!(conn.verdict, Verdict::Accept);

    // Localhost target.
    let mut conn = connection();
    let entity = conn.entity.as_mut().expect("entity");
    entity.ip = Some("127.0.0.1".parse().unwrap());
    entity.init(443);
    assert_eq!(entity.ip_scope, IpScope::HostLocal);
    check_split_tunneling(&mut conn, &FakeEnv::default());
    assert_eq!(conn.verdict, Verdict::Accept);
}

#[test]
fn requests_are_queued_and_stored_on_the_connection() {
    let mut conn = connection();
    let env = FakeEnv {
        proxy: "socks5://127.0.0.1:1080".to_string(),
        ..FakeEnv::default()
    };

    request_split_tunneling(&mut conn, &env).expect("request");

    let context = conn.split_tun_context.as_ref().expect("context");
    assert_eq!(context.interface, "portmaster0");
    assert_eq!(context.proxy, "socks5://127.0.0.1:1080");
    assert_eq!(
        env.requests.lock().expect("lock").as_slice(),
        &[(
            "portmaster0".to_string(),
            "socks5://127.0.0.1:1080".to_string()
        )]
    );
}

#[test]
fn requests_without_a_process_fail() {
    let mut conn = connection();
    conn.process = None;
    let error = request_split_tunneling(&mut conn, &FakeEnv::default())
        .expect_err("no process");
    assert_eq!(error, "no profile set");
}

#[test]
fn own_proxy_connections_are_detected() {
    let conn = connection();
    let mut env = FakeEnv {
        proxied: true,
        ..FakeEnv::default()
    };
    // The proxy runs in the core process.
    let mut proxy_conn = Connection {
        pid: env.own_pid,
        ip_protocol: protocol::TCP,
        ..conn
    };
    assert!(is_own_split_tunnel_proxy_connection(&proxy_conn, &env));

    // A normal process connection is not the proxy.
    proxy_conn.pid = 200;
    assert!(!is_own_split_tunnel_proxy_connection(&proxy_conn, &env));

    // Not proxied.
    env.proxied = false;
    proxy_conn.pid = env.own_pid;
    assert!(!is_own_split_tunnel_proxy_connection(&proxy_conn, &env));

    // Unsupported protocol.
    env.proxied = true;
    proxy_conn.ip_protocol = protocol::ICMP;
    assert!(!is_own_split_tunnel_proxy_connection(&proxy_conn, &env));

    // Proxy not ready.
    proxy_conn.ip_protocol = protocol::TCP;
    env.ready = false;
    assert!(!is_own_split_tunnel_proxy_connection(&proxy_conn, &env));
}
