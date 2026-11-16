#![allow(clippy::field_reassign_with_default)]
//! Behavior tests of the firewall deciders, derived from the Go branches of
//! `service/firewall/master.go` and `bypassing.go`.

use std::cell::{Cell, RefCell};
use std::net::IpAddr;
use std::sync::Arc;

use pm_config::Config;
use pm_database::Databases;
use pm_endpoints::EpResult;
use pm_firewall::*;
use pm_intel::{cache_interface, Entity, EntryResource, FilterLists, ListEntry, StaticLocations};
use pm_netenv::OnlineStatus;
use pm_network::{
    Connection, ConnectionType, LocalProfileInfo, ProfileLookup, Verdict,
};
use pm_process::Process;
use pm_profile::options::*;
use pm_profile::{
    GlobalProfileConfig, LayeredProfile, Profile, DEFAULT_ACTION_ASK, DEFAULT_ACTION_BLOCK,
    DEFAULT_ACTION_PERMIT, SOURCE_LOCAL,
};
use serde_json::{json, Map, Value};

const OPT_DEFAULT_ACTION: &str = CFG_OPTION_DEFAULT_ACTION_KEY;

struct AnySource;

impl ProfileLookup for AnySource {
    fn local_profile(&self, _process: &Process) -> Option<LocalProfileInfo> {
        None
    }
    fn profile_source(&self, _process: &Process, _key: &str) -> Option<String> {
        Some("local/test".to_string())
    }
}

#[derive(Default)]
struct TestCustom {
    loaded: bool,
    domains: Vec<String>,
    ips: Vec<IpAddr>,
    asns: Vec<u32>,
    countries: Vec<String>,
}

impl CustomLists for TestCustom {
    fn is_loaded(&self) -> bool {
        self.loaded
    }
    fn lookup_domain(&self, fqdn: &str, _sub: bool) -> (bool, String) {
        if self.domains.iter().any(|d| d == fqdn) {
            (true, format!("match-{fqdn}"))
        } else {
            (false, String::new())
        }
    }
    fn lookup_ip(&self, ip: IpAddr) -> bool {
        self.ips.contains(&ip)
    }
    fn lookup_asn(&self, asn: u32) -> bool {
        self.asns.contains(&asn)
    }
    fn lookup_country(&self, country: &str) -> bool {
        self.countries.iter().any(|c| c == country)
    }
}

struct TestEnv {
    own_pid: i32,
    my_ips: Vec<IpAddr>,
    my_ip_error: bool,
    local_net: Option<LocalNetwork>,
    multicast: Option<MulticastRequest>,
    status: OnlineStatus,
    connectivity: Vec<String>,
    resolver_disabled: bool,
    unbreak: Vec<String>,
    custom: TestCustom,
    prompts: Cell<u32>,
    bypass_reports: Cell<u32>,
    loaded_profiles: RefCell<Vec<String>>,
}

impl Default for TestEnv {
    fn default() -> TestEnv {
        TestEnv {
            own_pid: 4242,
            my_ips: Vec::new(),
            my_ip_error: false,
            local_net: None,
            multicast: None,
            status: OnlineStatus::Online,
            connectivity: Vec::new(),
            resolver_disabled: false,
            unbreak: Vec::new(),
            custom: TestCustom::default(),
            prompts: Cell::new(0),
            bypass_reports: Cell::new(0),
            loaded_profiles: RefCell::new(Vec::new()),
        }
    }
}

impl FirewallEnv for TestEnv {
    fn own_pid(&self) -> i32 {
        self.own_pid
    }
    fn is_my_ip(&self, ip: IpAddr) -> Result<bool, String> {
        if self.my_ip_error {
            return Err("boom".to_string());
        }
        Ok(self.my_ips.contains(&ip))
    }
    fn local_network(&self, _ip: IpAddr) -> Result<Option<LocalNetwork>, String> {
        Ok(self.local_net)
    }
    fn multicast_request(&self, _c: &Connection, _n: &LocalNetwork) -> Option<MulticastRequest> {
        self.multicast
    }
    fn online_status(&self) -> OnlineStatus {
        self.status
    }
    fn is_connectivity_domain(&self, domain: &str) -> bool {
        self.connectivity.iter().any(|d| d == domain)
    }
    fn resolver_disabled(&self) -> bool {
        self.resolver_disabled
    }
    fn report_secure_dns_bypass(&self, _p: Option<&Process>) {
        self.bypass_reports.set(self.bypass_reports.get() + 1);
    }
    fn unbreak_filter_list_ids(&self) -> Vec<String> {
        self.unbreak.clone()
    }
    fn custom_lists(&self) -> &dyn CustomLists {
        &self.custom
    }
    fn load_local_profile(&self, id: &str) -> Result<Profile, String> {
        self.loaded_profiles.borrow_mut().push(id.to_string());
        Err("not available".to_string())
    }
    fn prompt(&self, _conn: &mut Connection) {
        self.prompts.set(self.prompts.get() + 1);
    }
}

struct Fx {
    config: Arc<Config>,
    global: Arc<GlobalProfileConfig>,
    _databases: Arc<Databases>,
    lists: Arc<FilterLists>,
}

fn fx() -> Fx {
    let config = Config::new();
    register_options(&config).unwrap();
    let global = Arc::new(GlobalProfileConfig::new(&config));
    global.refresh().unwrap();
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .unwrap();
    databases
        .inject("cache", Box::new(pm_database::MemoryStorage::new()))
        .unwrap();
    let lists = Arc::new(FilterLists::with_capacity(
        cache_interface(&databases),
        100,
        10,
        5,
        20,
        20,
    ));
    lists.set_loaded(true);
    Fx {
        config,
        global,
        _databases: databases,
        lists,
    }
}

impl Fx {
    fn store(&self, domain: &str, sources: &[&str]) {
        self.lists
            .store_entry(
                &ListEntry {
                    type_: "domain".to_string(),
                    entity: domain.to_string(),
                    whitelist: false,
                    resources: sources
                        .iter()
                        .map(|s| EntryResource {
                            source_id: s.to_string(),
                            resource_id: "r1".to_string(),
                        })
                        .collect(),
                },
                1_700_000_000,
            )
            .unwrap();
    }

    fn profile(&self, config: Value) -> LayeredProfile {
        let local = Arc::new(Profile::new(Profile {
            source: SOURCE_LOCAL.to_string(),
            config: config.as_object().cloned().unwrap_or_else(Map::new),
            ..Profile::default()
        }));
        LayeredProfile::new(local, &self.config, &self.global)
    }

    fn entity(&self, domain: &str, ip: Option<&str>, port: u16) -> Entity {
        let mut e = Entity::new(Arc::clone(&self.lists), Arc::new(StaticLocations::new()));
        e.domain = domain.to_string();
        e.ip = ip.map(|i| i.parse().unwrap());
        e.protocol = 6;
        e.port = port;
        e.init(0);
        e
    }

    fn conn(&self, domain: &str, ip: Option<&str>, port: u16) -> Connection {
        let mut c = Connection::default();
        c.conn_type = ConnectionType::Ip;
        c.ip_protocol = 6;
        c.entity = Some(self.entity(domain, ip, port));
        c.process = Some(Arc::new(process(100)));
        c.profiles = Arc::new(AnySource);
        c
    }
}

fn process(pid: i32) -> Process {
    let mut p = Process::empty(&format!("{pid}-1"));
    p.pid = pid;
    p.name = "Firefox".to_string();
    p.path = "/usr/lib/firefox/firefox".to_string();
    p.exec_name = "firefox".to_string();
    p
}

fn dns_conn(fx: &Fx, domain: &str) -> Connection {
    let mut c = fx.conn(domain, None, 0);
    c.conn_type = ConnectionType::Dns;
    c
}

fn state(c: &Connection) -> (Verdict, String, String) {
    (c.verdict, c.reason.msg.clone(), c.reason.option_key.clone())
}

fn run(
    decider: Decider,
    conn: &mut Connection,
    p: &LayeredProfile,
    env: &TestEnv,
) -> (bool, Verdict, String, String) {
    let done = decider(conn, p, env);
    let (v, m, k) = state(conn);
    (done, v, m, k)
}

fn ent(c: &mut Connection) -> &mut Entity {
    c.entity.as_mut().unwrap()
}

#[test]
fn portmaster_connection() {
    let fx = fx();
    let p = fx.profile(json!({}));
    let mut env = TestEnv::default();

    // Other processes are ignored.
    let mut c = fx.conn("", Some("1.1.1.1"), 443);
    assert!(!check_portmaster_connection(&mut c, &p, &env));
    assert_eq!(c.verdict, Verdict::Undecided);

    // Own outgoing connection.
    let mut c = fx.conn("", Some("1.1.1.1"), 443);
    c.process = Some(Arc::new(process(env.own_pid)));
    assert!(check_portmaster_connection(&mut c, &p, &env));
    assert_eq!(
        state(&c),
        (Verdict::Accept, "connection by Portmaster".into(), "".into())
    );
    assert!(c.internal);

    // Inbound from a non-local IP is ignored, from an own IP granted.
    let mut c = fx.conn("", Some("1.1.1.1"), 443);
    c.process = Some(Arc::new(process(env.own_pid)));
    c.inbound = true;
    assert!(!check_portmaster_connection(&mut c, &p, &env));
    env.my_ips.push("1.1.1.1".parse().unwrap());
    assert!(check_portmaster_connection(&mut c, &p, &env));

    // A failing own-IP check does not grant.
    env.my_ip_error = true;
    let mut c = fx.conn("", Some("1.1.1.1"), 443);
    c.process = Some(Arc::new(process(env.own_pid)));
    c.inbound = true;
    assert!(!check_portmaster_connection(&mut c, &p, &env));
}

#[test]
fn broadcast_reply() {
    let fx = fx();
    let p = fx.profile(json!({}));
    let mut env = TestEnv::default();
    env.local_net = Some(LocalNetwork {
        ip: "192.168.1.0".parse().unwrap(),
        prefix_len: 24,
    });
    env.multicast = Some(MulticastRequest {
        protocol: 17,
        ip: "224.0.0.251".parse().unwrap(),
        port: 5353,
    });

    // Outbound is ignored.
    let mut c = fx.conn("", Some("192.168.1.5"), 5353);
    assert!(!check_if_broadcast_reply(&mut c, &p, &env));

    // Unidentified process is ignored.
    let mut c = fx.conn("", Some("192.168.1.5"), 5353);
    c.inbound = true;
    c.process = Some(Arc::new(process(-1)));
    assert!(!check_if_broadcast_reply(&mut c, &p, &env));

    // Not in a local network.
    let mut c = fx.conn("", Some("192.168.1.5"), 5353);
    c.inbound = true;
    let mut no_net = TestEnv::default();
    no_net.multicast = env.multicast;
    assert!(!check_if_broadcast_reply(&mut c, &p, &no_net));

    // No requesting connection.
    let mut none = TestEnv::default();
    none.local_net = env.local_net;
    assert!(!check_if_broadcast_reply(&mut c, &p, &none));

    // Matching request.
    assert!(check_if_broadcast_reply(&mut c, &p, &env));
    assert_eq!(
        state(&c),
        (
            Verdict::Accept,
            "response to multi/broadcast query to UDP/224.0.0.251:5353".into(),
            "".into()
        )
    );

    // IPv6 requests are bracketed.
    env.multicast = Some(MulticastRequest {
        protocol: 17,
        ip: "ff02::fb".parse().unwrap(),
        port: 5353,
    });
    assert!(check_if_broadcast_reply(&mut c, &p, &env));
    assert_eq!(c.reason.msg, "response to multi/broadcast query to UDP/[ff02::fb]:5353");
}

#[test]
fn connection_type() {
    let fx = fx();
    fx.store("p2p.example.", &["17-P2P"]);
    let env = TestEnv::default();

    // Inbound blocked, but not from localhost.
    let p = fx.profile(json!({"filter": {"blockInbound": true}}));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    c.inbound = true;
    assert_eq!(
        run(check_connection_type, &mut c, &p, &env),
        (
            true,
            Verdict::Drop,
            "inbound connections blocked".into(),
            CFG_OPTION_BLOCK_INBOUND_KEY.into()
        )
    );
    let mut c = fx.conn("", Some("127.0.0.1"), 443);
    c.inbound = true;
    assert!(!check_connection_type(&mut c, &p, &env));

    let p = fx.profile(json!({"filter": {"blockP2P": true, "blockInbound": false}}));

    // Filter list match.
    let mut c = fx.conn("p2p.example.", Some("8.8.8.8"), 443);
    ent(&mut c).load_lists();
    assert_eq!(
        run(check_connection_type, &mut c, &p, &env),
        (
            true,
            Verdict::Block,
            "P2P assistive infrastructure blocked based on filter list".into(),
            CFG_OPTION_BLOCK_P2P_KEY.into()
        )
    );

    // DNS requests only get the list check.
    let mut c = dns_conn(&fx, "ok.example.");
    assert!(!check_connection_type(&mut c, &p, &env));

    // Well known ports.
    for port in [3478, 5349] {
        let mut c = fx.conn("ok.example.", Some("8.8.8.8"), port);
        assert_eq!(
            run(check_connection_type, &mut c, &p, &env).2,
            "P2P assistive infrastructure blocked based on port"
        );
    }

    // Direct global connection without a domain.
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    assert_eq!(
        run(check_connection_type, &mut c, &p, &env).2,
        "direct connections (P2P) blocked"
    );

    // With a domain, or to the LAN, nothing happens; inbound is not checked.
    let mut c = fx.conn("ok.example.", Some("8.8.8.8"), 443);
    assert!(!check_connection_type(&mut c, &p, &env));
    let mut c = fx.conn("", Some("192.168.1.2"), 443);
    assert!(!check_connection_type(&mut c, &p, &env));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    c.inbound = true;
    assert!(!check_connection_type(&mut c, &p, &env));

    // Disabled options do nothing.
    let p = fx.profile(json!({}));
    let mut c = fx.conn("", Some("8.8.8.8"), 3478);
    assert!(!check_connection_type(&mut c, &p, &env));
}

#[test]
fn connection_scope() {
    let fx = fx();
    let env = TestEnv::default();

    // DNS: only blocked if both Internet and LAN are blocked.
    let p = fx.profile(json!({"filter": {"blockInternet": true}}));
    let mut c = dns_conn(&fx, "a.example.");
    assert!(!check_connection_scope(&mut c, &p, &env));
    let p = fx.profile(json!({"filter": {"blockInternet": true, "blockLAN": true}}));
    assert_eq!(
        run(check_connection_scope, &mut c, &p, &env),
        (
            true,
            Verdict::Block,
            "Internet and LAN access blocked".into(),
            CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY.into()
        )
    );

    let p = fx.profile(json!({"filter": {"blockInternet": true, "blockLAN": true, "blockLocal": true}}));
    let cases = [
        ("8.8.8.8", false, Verdict::Block, "Internet access blocked", CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY),
        ("8.8.8.8", true, Verdict::Drop, "Internet access blocked", CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY),
        ("230.0.0.1", false, Verdict::Block, "Internet access blocked", CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY),
        ("192.168.1.1", false, Verdict::Block, "LAN access blocked", CFG_OPTION_BLOCK_SCOPE_LAN_KEY),
        ("169.254.1.1", false, Verdict::Block, "LAN access blocked", CFG_OPTION_BLOCK_SCOPE_LAN_KEY),
        ("224.0.0.1", false, Verdict::Block, "LAN access blocked", CFG_OPTION_BLOCK_SCOPE_LAN_KEY),
        ("127.0.0.1", false, Verdict::Block, "Localhost access blocked", CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY),
        ("127.0.0.1", true, Verdict::Block, "Localhost access blocked", CFG_OPTION_BLOCK_SCOPE_LOCAL_KEY),
    ];
    for (ip, inbound, verdict, msg, key) in cases {
        let mut c = fx.conn("", Some(ip), 443);
        c.inbound = inbound;
        assert_eq!(
            run(check_connection_scope, &mut c, &p, &env),
            (true, verdict, msg.into(), key.into()),
            "{ip} inbound={inbound}"
        );
    }

    // Allowed scopes and undefined IPs are left to later deciders.
    let p = fx.profile(json!({}));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    assert!(!check_connection_scope(&mut c, &p, &env));
    let mut c = fx.conn("", None, 443);
    assert!(!check_connection_scope(&mut c, &fx.profile(json!({"filter": {"blockInternet": true}})), &env));
}

#[test]
fn endpoint_lists() {
    let fx = fx();
    let env = TestEnv::default();

    let p = fx.profile(json!({"filter": {"endpoints": ["- example.com", "+ good.org"]}}));
    let mut c = fx.conn("example.com.", Some("8.8.8.8"), 443);
    let (done, verdict, msg, key) = run(check_endpoint_lists, &mut c, &p, &env);
    assert!(done);
    assert_eq!(verdict, Verdict::Block);
    assert!(msg.starts_with("denied by rule: "), "{msg}");
    assert_eq!(key, CFG_OPTION_ENDPOINTS_KEY);
    assert_eq!(c.reason.context.as_ref().unwrap()["Filter"], "- example.com");

    let mut c = fx.conn("good.org.", Some("8.8.8.8"), 443);
    let (done, verdict, msg, _) = run(check_endpoint_lists, &mut c, &p, &env);
    assert!(done);
    assert_eq!(verdict, Verdict::Accept);
    assert!(msg.starts_with("allowed by rule: "), "{msg}");

    let mut c = fx.conn("other.net.", Some("8.8.8.8"), 443);
    assert!(!check_endpoint_lists(&mut c, &p, &env));
    assert_eq!(c.verdict, Verdict::Undecided);

    // Inbound uses the service endpoints and drops.
    let p = fx.profile(json!({"filter": {"serviceEndpoints": ["- *"], "endpoints": ["+ *"]}}));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    c.inbound = true;
    let (done, verdict, _, key) = run(check_endpoint_lists, &mut c, &p, &env);
    assert!(done);
    assert_eq!(verdict, Verdict::Drop);
    assert_eq!(key, CFG_OPTION_SERVICE_ENDPOINTS_KEY);
}

#[test]
fn endpoint_lists_for_system_resolver_dns() {
    let fx = fx();
    let env = TestEnv::default();
    let mut resolver = process(77);
    resolver.primary_profile_id = "local/_system-resolver".to_string();
    let resolver = Arc::new(resolver);

    // Only the local profile is consulted: a global deny does not apply.
    fx.config
        .set_config_option(CFG_OPTION_ENDPOINTS_KEY, json!(["- example.com"]))
        .unwrap();
    fx.global.refresh().unwrap();
    let p = fx.profile(json!({}));
    let mut c = dns_conn(&fx, "example.com.");
    c.process = Some(Arc::clone(&resolver));
    assert!(!check_endpoint_lists(&mut c, &p, &env));

    // The local profile's rules decide.
    let p = fx.profile(json!({"filter": {"endpoints": ["- example.com"]}}));
    let mut c = dns_conn(&fx, "example.com.");
    c.process = Some(Arc::clone(&resolver));
    let (done, verdict, _, key) = run(check_endpoint_lists, &mut c, &p, &env);
    assert!(done);
    assert_eq!(verdict, Verdict::Block);
    assert_eq!(key, CFG_OPTION_ENDPOINTS_KEY);

    let p = fx.profile(json!({"filter": {"endpoints": ["+ example.com"]}}));
    let mut c = dns_conn(&fx, "example.com.");
    c.process = Some(resolver);
    assert!(check_endpoint_lists(&mut c, &p, &env));
    assert_eq!(c.verdict, Verdict::Accept);
}

#[test]
fn invalid_ip() {
    let fx = fx();
    let p = fx.profile(json!({}));
    let env = TestEnv::default();

    let mut c = fx.conn("", None, 443);
    assert_eq!(
        run(check_invalid_ip, &mut c, &p, &env),
        (true, Verdict::Block, "invalid IP".into(), "".into())
    );
    let mut c = fx.conn("", None, 443);
    c.inbound = true;
    assert_eq!(run(check_invalid_ip, &mut c, &p, &env).1, Verdict::Drop);

    // Only IP connections are checked, valid IPs pass.
    let mut c = dns_conn(&fx, "a.example.");
    assert!(!check_invalid_ip(&mut c, &p, &env));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    assert!(!check_invalid_ip(&mut c, &p, &env));
}

#[test]
fn resolver_scope() {
    let fx = fx();
    let env = TestEnv::default();
    let p = fx.profile(json!({"filter": {"removeOutOfScopeDNS": true}}));
    let resolver = |ip: &str| pm_resolver_info(ip);

    let mut c = fx.conn("", Some("192.168.1.2"), 443);
    c.resolver = Some(resolver("8.8.8.8"));
    assert_eq!(
        run(check_resolver_scope, &mut c, &p, &env),
        (
            true,
            Verdict::Block,
            "DNS server horizon violation: global DNS server returned local IP address".into(),
            CFG_OPTION_REMOVE_OUT_OF_SCOPE_DNS_KEY.into()
        )
    );
    let mut c = fx.conn("", Some("127.0.0.1"), 443);
    c.resolver = Some(resolver("8.8.8.8"));
    assert!(check_resolver_scope(&mut c, &p, &env));
    let mut c = fx.conn("", Some("127.0.0.1"), 443);
    c.resolver = Some(resolver("192.168.1.1"));
    assert_eq!(
        run(check_resolver_scope, &mut c, &p, &env).2,
        "DNS server horizon violation: LAN DNS server returned localhost IP address"
    );

    // Not applicable: LAN resolver with LAN IP, no resolver, DNS type, option off.
    let mut c = fx.conn("", Some("192.168.1.2"), 443);
    c.resolver = Some(resolver("192.168.1.1"));
    assert!(!check_resolver_scope(&mut c, &p, &env));
    let mut c = fx.conn("", Some("127.0.0.1"), 443);
    assert!(!check_resolver_scope(&mut c, &p, &env));
    let mut c = dns_conn(&fx, "a.");
    c.resolver = Some(resolver("8.8.8.8"));
    assert!(!check_resolver_scope(&mut c, &p, &env));
    let off = fx.profile(json!({"filter": {"removeOutOfScopeDNS": false}}));
    let mut c = fx.conn("", Some("127.0.0.1"), 443);
    c.resolver = Some(resolver("8.8.8.8"));
    assert!(!check_resolver_scope(&mut c, &off, &env));
}

fn pm_resolver_info(ip: &str) -> pm_resolver::ResolverInfo {
    let ip: IpAddr = ip.parse().unwrap();
    pm_resolver::ResolverInfo {
        ip: Some(ip),
        ip_scope: pm_netstate::ip_scope(ip),
        name: String::new(),
        r#type: String::new(),
        source: String::new(),
        domain: String::new(),
        port: 0,
    }
}

#[test]
fn connectivity_domain() {
    let fx = fx();
    let mut env = TestEnv::default();
    env.status = OnlineStatus::Portal;
    env.connectivity.push("connectivity.example.".to_string());
    let p = fx.profile(json!({}));

    let mut c = fx.conn("connectivity.example.", Some("8.8.8.8"), 443);
    assert_eq!(
        run(check_connectivity_domain, &mut c, &p, &env),
        (
            true,
            Verdict::Accept,
            "special grant for connectivity domain during network bootstrap".into(),
            "".into()
        )
    );

    let mut c = fx.conn("other.example.", Some("8.8.8.8"), 443);
    assert!(!check_connectivity_domain(&mut c, &p, &env));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    assert!(!check_connectivity_domain(&mut c, &p, &env));
    let mut c = fx.conn("connectivity.example.", Some("8.8.8.8"), 443);
    c.inbound = true;
    assert!(!check_connectivity_domain(&mut c, &p, &env));
    let blocked = fx.profile(json!({"filter": {"blockInternet": true}}));
    let mut c = fx.conn("connectivity.example.", Some("8.8.8.8"), 443);
    assert!(!check_connectivity_domain(&mut c, &blocked, &env));

    // Better than Portal: no special grant.
    env.status = OnlineStatus::SemiOnline;
    let mut c = fx.conn("connectivity.example.", Some("8.8.8.8"), 443);
    assert!(!check_connectivity_domain(&mut c, &p, &env));
    env.status = OnlineStatus::Offline;
    assert!(check_connectivity_domain(&mut c, &p, &env));
}

#[test]
fn bypass_prevention_rules() {
    let fx = fx();
    let env = TestEnv::default();

    // Inbound and ICMP are excluded.
    let mut c = fx.conn("", Some("8.8.8.8"), 53);
    c.inbound = true;
    assert_eq!(prevent_bypassing(&mut c, &env).0, EpResult::NoMatch);
    for proto in [1, 58] {
        let mut c = fx.conn("", Some("8.8.8.8"), 53);
        c.entity.as_mut().unwrap().protocol = proto;
        assert_eq!(prevent_bypassing(&mut c, &env).0, EpResult::NoMatch);
    }

    // Firefox canary domain.
    let mut c = fx.conn("USE-application-dns.net.", Some("8.8.8.8"), 443);
    let (result, reason, responder) = prevent_bypassing(&mut c, &env);
    assert_eq!(result, EpResult::Denied);
    assert_eq!(reason, "blocked canary domain to prevent enabling of DNS-over-HTTPs");
    assert_eq!(responder, Some(BypassResponder::NxDomain));

    // System resolver DNS requests are excluded.
    let mut sys = process(77);
    sys.primary_profile_id = "local/_system-resolver".to_string();
    let mut c = dns_conn(&fx, "a.example.");
    c.entity = Some(fx.entity("a.example.", Some("8.8.8.8"), 53));
    c.process = Some(Arc::new(sys.clone()));
    assert_eq!(prevent_bypassing(&mut c, &env).0, EpResult::NoMatch);

    // Port 53 and 853, resolver list.
    let mut c = fx.conn("", Some("8.8.8.8"), 53);
    let (result, reason, responder) = prevent_bypassing(&mut c, &env);
    assert_eq!(result, EpResult::Denied);
    assert_eq!(reason, "blocked DNS query, manual dns setup required");
    assert_eq!(responder, Some(BypassResponder::BlockIp));

    let mut c = fx.conn("", Some("8.8.8.8"), 853);
    let (result, reason, responder) = prevent_bypassing(&mut c, &env);
    assert_eq!(result, EpResult::Denied);
    assert_eq!(reason, "blocked rogue connection to DNS resolver");
    assert_eq!(responder, Some(BypassResponder::BlockIp));
    assert_eq!(env.bypass_reports.get(), 1);

    fx.store("doh.example.", &["17-DNS"]);
    let mut c = fx.conn("doh.example.", Some("8.8.8.8"), 443);
    ent(&mut c).load_lists();
    assert_eq!(prevent_bypassing(&mut c, &env).2, Some(BypassResponder::BlockIp));
    assert_eq!(env.bypass_reports.get(), 2);

    let mut c = fx.conn("fine.example.", Some("8.8.8.8"), 443);
    ent(&mut c).load_lists();
    assert_eq!(prevent_bypassing(&mut c, &env), (EpResult::NoMatch, String::new(), None));
}

#[test]
fn bypass_prevention_with_disabled_resolver() {
    let fx = fx();
    let mut env = TestEnv::default();
    env.resolver_disabled = true;

    // System resolver may connect anywhere.
    let mut sys = process(77);
    sys.primary_profile_id = "local/_system-resolver".to_string();
    let mut c = fx.conn("", Some("8.8.8.8"), 853);
    c.process = Some(Arc::new(sys));
    assert_eq!(prevent_bypassing(&mut c, &env).0, EpResult::NoMatch);

    // Local port 53 is allowed.
    let mut c = fx.conn("", Some("127.0.0.53"), 53);
    c.ip_protocol = 6;
    assert_eq!(prevent_bypassing(&mut c, &env).0, EpResult::NoMatch);
    assert!(!c.inspecting);

    // Outgoing UDP to port 53 is allowed for inspection.
    let mut c = fx.conn("", Some("8.8.8.8"), 53);
    c.ip_protocol = 17;
    assert_eq!(prevent_bypassing(&mut c, &env).0, EpResult::NoMatch);
    assert!(c.inspecting);

    // TCP to a remote port 53 is still blocked.
    let mut c = fx.conn("", Some("8.8.8.8"), 53);
    c.ip_protocol = 6;
    assert_eq!(prevent_bypassing(&mut c, &env).0, EpResult::Denied);
}

#[test]
fn looks_like_outgoing_dns() {
    let fx = fx();
    let mut c = fx.conn("", Some("8.8.8.8"), 53);
    c.ip_protocol = 17;
    assert!(looks_like_outgoing_dns_request(&c));
    c.inbound = true;
    assert!(!looks_like_outgoing_dns_request(&c));
    c.inbound = false;
    c.ip_protocol = 6;
    assert!(!looks_like_outgoing_dns_request(&c));
    c.ip_protocol = 17;
    ent(&mut c).port = 54;
    assert!(!looks_like_outgoing_dns_request(&c));
}

#[test]
fn bypass_prevention_decider() {
    let fx = fx();
    let env = TestEnv::default();
    let on = fx.profile(json!({"filter": {"preventBypassing": true}}));
    let off = fx.profile(json!({"filter": {"preventBypassing": false}}));

    let mut c = fx.conn("", Some("8.8.8.8"), 53);
    assert_eq!(
        run(check_bypass_prevention, &mut c, &on, &env),
        (
            true,
            Verdict::Block,
            "bypass prevention: blocked DNS query, manual dns setup required".into(),
            CFG_OPTION_PREVENT_BYPASSING_KEY.into()
        )
    );
    let mut c = fx.conn("", Some("8.8.8.8"), 53);
    assert!(!check_bypass_prevention(&mut c, &off, &env));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    assert!(!check_bypass_prevention(&mut c, &on, &env));
}

#[test]
fn filter_lists() {
    let fx = fx();
    fx.store("evil.example.", &["MAL"]);
    fx.store("evil2.example.", &["MAL", "UNBREAK1"]);
    let p = fx.profile(json!({"filter": {"lists": ["MAL", "UNBREAK1"]}}));
    let mut env = TestEnv::default();

    let mut c = fx.conn("evil.example.", Some("8.8.8.8"), 443);
    ent(&mut c).load_lists();
    let (done, verdict, msg, key) = run(check_filter_lists, &mut c, &p, &env);
    assert!(done);
    assert_eq!(verdict, Verdict::Block);
    assert!(msg.contains("evil.example."), "{msg}");
    assert!(msg.contains("MAL"), "{msg}");
    assert_eq!(key, CFG_OPTION_FILTER_LISTS_KEY);
    assert!(c.reason.context.as_ref().unwrap()["filterlists"].is_array());

    // No match.
    let mut c = fx.conn("fine.example.", Some("8.8.8.8"), 443);
    ent(&mut c).load_lists();
    assert!(!check_filter_lists(&mut c, &p, &env));

    // Unbreak list cancels the block.
    env.unbreak = vec!["UNBREAK1".to_string()];
    let mut c = fx.conn("evil2.example.", Some("8.8.8.8"), 443);
    ent(&mut c).load_lists();
    assert!(!check_filter_lists(&mut c, &p, &env));
    assert_eq!(c.verdict, Verdict::Undecided);
    let mut c = fx.conn("evil.example.", Some("8.8.8.8"), 443);
    ent(&mut c).load_lists();
    assert!(check_filter_lists(&mut c, &p, &env));
}

#[test]
fn custom_filter_list() {
    let fx = fx();
    let p = fx.profile(json!({"filter": {"includeCNAMEs": true}}));
    let key = "filter/customListFile";

    // Not loaded: nothing happens.
    let env = TestEnv::default();
    let mut c = fx.conn("bad.example.", Some("1.2.3.4"), 443);
    assert!(!check_custom_filter_list(&mut c, &p, &env));

    let mut env = TestEnv::default();
    env.custom.loaded = true;
    env.custom.domains = vec!["bad.example.".into(), "alias.example.".into()];
    env.custom.ips = vec!["6.6.6.6".parse().unwrap()];
    env.custom.asns = vec![666];
    env.custom.countries = vec!["XX".into()];

    let mut c = fx.conn("bad.example.", Some("1.2.3.4"), 443);
    assert_eq!(
        run(check_custom_filter_list, &mut c, &p, &env),
        (
            true,
            Verdict::Block,
            "domain bad.example. matches match-bad.example. in custom filter list".into(),
            key.into()
        )
    );

    let mut c = fx.conn("ok.example.", Some("1.2.3.4"), 443);
    ent(&mut c).cname = vec!["alias.example.".into()];
    assert_eq!(
        run(check_custom_filter_list, &mut c, &p, &env).2,
        "domain alias (CNAME) alias.example. matches match-alias.example. in custom filter list"
    );
    // CNAMEs are only checked if enabled.
    let no_cname = fx.profile(json!({"filter": {"includeCNAMEs": false}}));
    let mut c = fx.conn("ok.example.", Some("1.2.3.4"), 443);
    ent(&mut c).cname = vec!["alias.example.".into()];
    assert!(!check_custom_filter_list(&mut c, &no_cname, &env));

    let mut c = fx.conn("ok.example.", Some("6.6.6.6"), 443);
    assert_eq!(
        run(check_custom_filter_list, &mut c, &p, &env).2,
        "IP address is in the custom filter list"
    );
    let mut c = fx.conn("ok.example.", Some("1.2.3.4"), 443);
    ent(&mut c).asn = 666;
    assert_eq!(
        run(check_custom_filter_list, &mut c, &p, &env).2,
        "AS is in the custom filter list"
    );
    let mut c = fx.conn("ok.example.", Some("1.2.3.4"), 443);
    ent(&mut c).country = "XX".into();
    assert_eq!(
        run(check_custom_filter_list, &mut c, &p, &env).2,
        "country is in the custom filter list"
    );
    let mut c = fx.conn("ok.example.", Some("1.2.3.4"), 443);
    assert!(!check_custom_filter_list(&mut c, &p, &env));
}

#[test]
fn domain_heuristics() {
    let fx = fx();
    let env = TestEnv::default();
    let on = fx.profile(json!({"filter": {"domainHeuristics": true}}));
    let off = fx.profile(json!({"filter": {"domainHeuristics": false}}));

    let dga = "1234567890123456789a0123456789012345678.com.";
    let mut c = fx.conn(dga, Some("8.8.8.8"), 443);
    assert_eq!(
        run(check_domain_heuristics, &mut c, &on, &env),
        (
            true,
            Verdict::Block,
            "possible DGA domain commonly used by malware".into(),
            CFG_OPTION_DOMAIN_HEURISTICS_KEY.into()
        )
    );
    let mut c = fx.conn(dga, Some("8.8.8.8"), 443);
    assert!(!check_domain_heuristics(&mut c, &off, &env));

    // No domain, normal domain, TLD only.
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    assert!(!check_domain_heuristics(&mut c, &on, &env));
    let mut c = fx.conn("www.google.com.", Some("8.8.8.8"), 443);
    assert!(!check_domain_heuristics(&mut c, &on, &env));
    let mut c = fx.conn("com.", Some("8.8.8.8"), 443);
    assert!(!check_domain_heuristics(&mut c, &on, &env));

    // Very long domains are checked as a whole (Go checks the first
    // len(eTLD+1) bytes).
    let tunnel = format!("1a2b3c4d5e6{}.example.com.", "q".repeat(100));
    let mut c = fx.conn(&tunnel, Some("8.8.8.8"), 443);
    assert_eq!(
        run(check_domain_heuristics, &mut c, &on, &env).2,
        "possible data tunnel for covert communication and protection bypassing"
    );
    let long_ok = format!("{}.example.com.", "q".repeat(120));
    let mut c = fx.conn(&long_ok, Some("8.8.8.8"), 443);
    assert!(!check_domain_heuristics(&mut c, &on, &env));
}

#[test]
fn auto_permit_related() {
    let fx = fx();
    let env = TestEnv::default();
    // The option is beta; it is ignored on the stable release level.
    fx.config
        .set_config_option(pm_config::RELEASE_LEVEL_KEY, json!("beta"))
        .unwrap();
    let ask = fx.profile(json!({"filter": {"defaultAction": "ask", "disableAutoPermit": false}}));

    let mut c = fx.conn("www.firefox.com.", Some("8.8.8.8"), 443);
    assert_eq!(
        run(check_auto_permit_related, &mut c, &ask, &env),
        (
            true,
            Verdict::Accept,
            "auto allowed: domain is related to process: firefox is related to firefox".into(),
            CFG_OPTION_DISABLE_AUTO_PERMIT_KEY.into()
        )
    );

    // Unrelated domain, no domain, unknown process.
    let mut c = fx.conn("www.example.com.", Some("8.8.8.8"), 443);
    assert!(!check_auto_permit_related(&mut c, &ask, &env));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    assert!(!check_auto_permit_related(&mut c, &ask, &env));
    let mut c = fx.conn("www.firefox.com.", Some("8.8.8.8"), 443);
    c.process = Some(Arc::new(process(-1)));
    assert!(!check_auto_permit_related(&mut c, &ask, &env));

    // Disabled by the default action permit or the option.
    let permit = fx.profile(json!({"filter": {"defaultAction": "permit"}}));
    let mut c = fx.conn("www.firefox.com.", Some("8.8.8.8"), 443);
    assert!(!check_auto_permit_related(&mut c, &permit, &env));
    let disabled = fx.profile(json!({"filter": {"defaultAction": "ask", "disableAutoPermit": true}}));
    assert!(disabled.disable_auto_permit());
    assert!(!check_auto_permit_related(&mut c, &disabled, &env));

    // Matches by process name and exec name too (similarity above 0.5).
    let mut c = fx.conn("firefx.example.", Some("8.8.8.8"), 443);
    assert!(check_auto_permit_related(&mut c, &ask, &env));
}

#[test]
fn decider_order_is_fixed() {
    assert_eq!(DEFAULT_DECIDERS.len(), 13);
    let expected: [Decider; 13] = [
        check_portmaster_connection,
        check_if_broadcast_reply,
        check_connection_type,
        check_connection_scope,
        check_endpoint_lists,
        check_invalid_ip,
        check_resolver_scope,
        check_connectivity_domain,
        check_bypass_prevention,
        check_filter_lists,
        check_custom_filter_list,
        check_domain_heuristics,
        check_auto_permit_related,
    ];
    for (got, want) in DEFAULT_DECIDERS.iter().zip(expected) {
        assert_eq!(*got as usize, want as usize);
    }
}

#[test]
fn earlier_decider_wins() {
    let fx = fx();
    fx.store("evil.example.", &["MAL"]);
    let env = TestEnv::default();

    // Scope (4) before endpoints (5).
    let p = fx.profile(json!({"filter": {"blockInternet": true, "endpoints": ["+ *"]}}));
    let mut c = fx.conn("evil.example.", Some("8.8.8.8"), 443);
    decide_on_connection(&mut c, Some(&p), &env);
    assert_eq!(
        state(&c),
        (
            Verdict::Block,
            "Internet access blocked".into(),
            CFG_OPTION_BLOCK_SCOPE_INTERNET_KEY.into()
        )
    );

    // Endpoints (5) before filter lists (10).
    let p = fx.profile(json!({"filter": {"endpoints": ["+ evil.example"]}}));
    let mut c = fx.conn("evil.example.", Some("8.8.8.8"), 443);
    decide_on_connection(&mut c, Some(&p), &env);
    assert_eq!(c.verdict, Verdict::Accept);
    assert!(c.reason.msg.starts_with("allowed by rule"));

    // Without an endpoint rule the filter list decides.
    let p = fx.profile(json!({}));
    let mut c = fx.conn("evil.example.", Some("8.8.8.8"), 443);
    decide_on_connection(&mut c, Some(&p), &env);
    assert_eq!(c.verdict, Verdict::Block);
    assert_eq!(c.reason.option_key, CFG_OPTION_FILTER_LISTS_KEY);
}

#[test]
fn missing_profile_is_denied() {
    let fx = fx();
    let env = TestEnv::default();
    let mut c = fx.conn("a.example.", Some("8.8.8.8"), 443);
    decide_on_connection(&mut c, None, &env);
    assert_eq!(
        state(&c),
        (Verdict::Block, "unknown process or profile".into(), "".into())
    );
    let mut c = fx.conn("a.example.", Some("8.8.8.8"), 443);
    c.inbound = true;
    decide_on_connection(&mut c, None, &env);
    assert_eq!(c.verdict, Verdict::Drop);
}

#[test]
fn default_action_tail() {
    let fx = fx();
    let env = TestEnv::default();
    let cases = [
        ("permit", Verdict::Accept, "allowed by default action"),
        ("block", Verdict::Block, "blocked by default action"),
    ];
    for (action, verdict, msg) in cases {
        let p = fx.profile(json!({"filter": {"defaultAction": action}}));
        let mut c = fx.conn("zzz.example.", Some("8.8.8.8"), 443);
        decide_on_connection(&mut c, Some(&p), &env);
        assert_eq!(
            state(&c),
            (verdict, msg.into(), OPT_DEFAULT_ACTION.into()),
            "{action}"
        );
    }

    // Inbound default block drops.
    let p = fx.profile(json!({"filter": {"defaultAction": "block"}}));
    let mut c = fx.conn("", Some("8.8.8.8"), 443);
    c.inbound = true;
    decide_on_connection(&mut c, Some(&p), &env);
    assert_eq!(c.verdict, Verdict::Drop);

    // Ask prompts once, and not again for decided connections.
    let p = fx.profile(json!({"filter": {"defaultAction": "ask"}}));
    let mut c = fx.conn("zzz.example.", Some("8.8.8.8"), 443);
    decide_on_connection(&mut c, Some(&p), &env);
    assert_eq!(env.prompts.get(), 1);
    assert_eq!(c.verdict, Verdict::Undecided);
    c.verdict = Verdict::Accept;
    decide_on_connection(&mut c, Some(&p), &env);
    assert_eq!(env.prompts.get(), 1);

    let _ = (DEFAULT_ACTION_ASK, DEFAULT_ACTION_BLOCK, DEFAULT_ACTION_PERMIT);
}

#[test]
fn dns_requests_are_accepted_by_default() {
    let fx = fx();
    let env = TestEnv::default();
    for action in ["block", "ask", "permit"] {
        let p = fx.profile(json!({"filter": {"defaultAction": action}}));
        let mut c = dns_conn(&fx, "zzz.example.");
        decide_on_connection(&mut c, Some(&p), &env);
        assert_eq!(
            state(&c),
            (Verdict::Accept, "allowing dns request".into(), "".into()),
            "{action}"
        );
    }
    assert_eq!(env.prompts.get(), 0);
}

#[test]
fn profile_update_resets_lists_and_revision() {
    let fx = fx();
    let env = TestEnv::default();
    let p = fx.profile(json!({"filter": {"defaultAction": "block"}}));
    let mut c = fx.conn("zzz.example.", Some("8.8.8.8"), 443);
    decide_on_connection(&mut c, Some(&p), &env);
    assert_eq!(c.profile_revision_counter, p.revision());
    assert!(c.save_when_finished);

    // An outdated local profile is reloaded through the environment.
    p.local_profile().mark_outdated();
    assert!(p.needs_update());
    let mut c = fx.conn("zzz.example.", Some("8.8.8.8"), 443);
    decide_on_connection(&mut c, Some(&p), &env);
    assert!(!env.loaded_profiles.borrow().is_empty());
    assert!(c.save_when_finished);
    assert_eq!(c.verdict, Verdict::Block);
}

#[test]
fn preauth_ports_registry() {
    let ports = PreAuthPorts::new();
    ports.register(17, 1234);
    assert!(ports.is_pre_authenticated(17, 1234));
    assert!(!ports.is_pre_authenticated(17, 1234));
}
