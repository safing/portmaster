#![allow(clippy::field_reassign_with_default)]
//! DNS response filtering, the IP to domain cache and the nameserver hook,
//! derived from Go `dns.go`.

use std::net::{IpAddr, Ipv4Addr};
use std::str::FromStr;
use std::sync::Arc;
use std::time::SystemTime;

use hickory_proto::rr::rdata::{A, CNAME};
use hickory_proto::rr::{Name, RData, Record, RecordType};
use pm_config::Config;
use pm_database::{Databases, MemoryStorage};
use pm_firewall::dns_filter::{
    FirewallDnsHook, IpInfoDomains, filter_dns_response, filter_dns_section, may_block_cnames,
};
use pm_firewall::packet::{LayeredProfileLookup, PacketEnv};
use pm_firewall::*;
use pm_intel::{Entity, FilterLists, StaticLocations, cache_interface};
use pm_nameserver::FirewallHook;
use pm_netenv::OnlineStatus;
use pm_network::{
    DomainLookup, Environment, LocalProfileInfo, Network, PacketInfo, ProcessResolver,
    ProfileLookup, SharedConnection, Verdict,
};
use pm_process::Process;
use pm_profile::options::register_options;
use pm_profile::{GlobalProfileConfig, LayeredProfile, Profile, SOURCE_LOCAL};
use pm_resolver::ipinfo::ip_info_interface_options;
use pm_resolver::{IpInfoStore, IpScope, Query, RRCache, ResolverInfo};
use serde_json::{Map, Value, json};

const OWN_PID: i32 = 999;

fn ip(s: &str) -> IpAddr {
    s.parse().unwrap()
}

struct FakeResolver;

impl ProcessResolver for FakeResolver {
    fn pid_of_connection(&self, info: &mut PacketInfo) -> (i32, bool) {
        (4242, info.inbound)
    }
    fn process(&self, pid: i32) -> Arc<Process> {
        let mut p = Process::empty(&format!("{pid}-1"));
        p.pid = pid;
        p.name = "firefox".to_string();
        Arc::new(p)
    }
    fn network_host(&self, _ip: IpAddr) -> Arc<Process> {
        self.process(-255)
    }
    fn exists(&self, _info: &PacketInfo, _now: SystemTime) -> bool {
        true
    }
    fn process_from_storage(&self, _key: &str) -> Option<Arc<Process>> {
        None
    }
    fn all_processes(&self) -> Vec<Arc<Process>> {
        Vec::new()
    }
}

struct Profiles {
    layered: Arc<LayeredProfile>,
}

impl ProfileLookup for Profiles {
    fn local_profile(&self, _p: &Process) -> Option<LocalProfileInfo> {
        Some(LocalProfileInfo {
            id: "p1".into(),
            name: "Test".into(),
            source: SOURCE_LOCAL.into(),
            revision_cnt: 1,
            ..LocalProfileInfo::default()
        })
    }
    fn profile_source(&self, _p: &Process, _key: &str) -> Option<String> {
        Some("local/p1".into())
    }
}

impl LayeredProfileLookup for Profiles {
    fn layered_profile(&self, _p: &Process) -> Option<Arc<LayeredProfile>> {
        Some(Arc::clone(&self.layered))
    }
}

struct TestEnv;

impl FirewallEnv for TestEnv {
    fn own_pid(&self) -> i32 {
        OWN_PID
    }
    fn is_my_ip(&self, _ip: IpAddr) -> Result<bool, String> {
        Ok(false)
    }
    fn local_network(&self, _ip: IpAddr) -> Result<Option<LocalNetwork>, String> {
        Ok(None)
    }
    fn multicast_request(
        &self,
        _c: &pm_network::Connection,
        _n: &LocalNetwork,
    ) -> Option<MulticastRequest> {
        None
    }
    fn online_status(&self) -> OnlineStatus {
        OnlineStatus::Online
    }
    fn is_connectivity_domain(&self, _d: &str) -> bool {
        false
    }
    fn resolver_disabled(&self) -> bool {
        false
    }
    fn unbreak_filter_list_ids(&self) -> Vec<String> {
        Vec::new()
    }
    fn custom_lists(&self) -> &dyn CustomLists {
        &NoCustomLists
    }
    fn load_local_profile(&self, _id: &str) -> Result<Profile, String> {
        Err("unavailable".into())
    }
    fn prompt(&self, _conn: &mut pm_network::Connection) {}
}

impl PacketEnv for TestEnv {
    fn filter_enabled(&self) -> bool {
        true
    }
    fn permanent_verdicts(&self) -> bool {
        true
    }
    fn dns_query_interception(&self) -> bool {
        false
    }
}

struct Fx {
    hook: FirewallDnsHook,
    network: Arc<Network>,
    layered: Arc<LayeredProfile>,
    store: Arc<IpInfoStore>,
    _databases: Arc<Databases>,
}

fn fx(profile: Value) -> Fx {
    let config = Config::new();
    register_options(&config).unwrap();
    let global = Arc::new(GlobalProfileConfig::new(&config));
    global.refresh().unwrap();
    let databases = Arc::new(Databases::without_disk());
    databases.register_injected("cache", "Cache").unwrap();
    databases
        .inject("cache", Box::new(MemoryStorage::new()))
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
    let locations = Arc::new(StaticLocations::new());
    let factory: Arc<dyn Fn() -> Entity + Send + Sync> =
        Arc::new(move || Entity::new(Arc::clone(&lists), locations.clone()));

    let local = Arc::new(Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        id: "p1".to_string(),
        config: profile.as_object().cloned().unwrap_or_else(Map::new),
        ..Profile::default()
    }));
    let layered = Arc::new(LayeredProfile::new(local, &config, &global));
    let profiles = Arc::new(Profiles {
        layered: Arc::clone(&layered),
    });

    let store = Arc::new(IpInfoStore::new(
        databases.interface(ip_info_interface_options()),
    ));
    let network_env = Environment::new(Arc::new(FakeResolver), factory)
        .with_profiles(profiles.clone())
        .with_domains(Arc::new(IpInfoDomains {
            store: Arc::clone(&store),
        }));
    let network = Network::new(network_env);
    let hook = FirewallDnsHook::new(
        Arc::new(TestEnv),
        profiles,
        Arc::clone(&store),
        Some(Arc::clone(&network)),
    );
    Fx {
        hook,
        network,
        layered,
        store,
        _databases: databases,
    }
}

fn dns_conn(fx: &Fx, fqdn: &str) -> SharedConnection {
    fx.network
        .new_connection_from_dns_request(fqdn, Vec::new(), "1-1", ip("127.0.0.1"), 43210)
}

fn a(name: &str, addr: &str) -> Record {
    let Ok(IpAddr::V4(v4)) = IpAddr::from_str(addr) else {
        panic!("not v4")
    };
    Record::from_rdata(Name::from_str(name).unwrap(), 17, RData::A(A(v4)))
}

fn cname(name: &str, target: &str) -> Record {
    Record::from_rdata(
        Name::from_str(name).unwrap(),
        17,
        RData::CNAME(CNAME(Name::from_str(target).unwrap())),
    )
}

fn response(domain: &str, scope: IpScope, answer: Vec<Record>) -> RRCache {
    RRCache {
        domain: domain.into(),
        question: RecordType::A,
        rcode: 0,
        answer,
        ns: vec![],
        extra: vec![],
        expires: 1_700_000_060,
        resolver: Some(ResolverInfo {
            name: "test".into(),
            r#type: "dns".into(),
            source: "config".into(),
            ip: Some(Ipv4Addr::new(9, 9, 9, 9).into()),
            domain: String::new(),
            ip_scope: scope,
            port: 53,
        }),
        served_from_cache: false,
        requesting_new: false,
        is_backup: false,
        filtered: false,
        filtered_entries: vec![],
        modified: 0,
    }
}

fn block_internet() -> Value {
    json!({"filter": {"defaultAction": "permit", "blockInternet": true}})
}

fn permit() -> Value {
    json!({"filter": {"defaultAction": "permit"}})
}

fn run_filter(fx: &Fx, conn: &SharedConnection, rr: RRCache, sys: bool) -> RRCache {
    let mut guard = conn.lock().unwrap();
    filter_dns_response(&mut guard, &fx.layered, rr, sys, OWN_PID)
}

#[test]
fn blocked_scope_record_is_removed_and_all_removed_blocks() {
    let fx = fx(block_internet());
    let conn = dns_conn(&fx, "example.com.");
    let rr = response(
        "example.com.",
        IpScope::Global,
        vec![
            cname("example.com.", "www.example.net."),
            a("www.example.net.", "93.184.216.34"),
        ],
    );
    let out = run_filter(&fx, &conn, rr, false);

    // The CNAME stays, the address is gone.
    assert_eq!(out.answer.len(), 1);
    assert!(matches!(out.answer[0].data, RData::CNAME(_)));
    assert!(out.filtered);
    assert_eq!(
        out.filtered_entries,
        ["www.example.net. 17 IN A 93.184.216.34"]
    );
    let guard = conn.lock().unwrap();
    assert_eq!(guard.verdict, Verdict::Block);
    assert_eq!(guard.reason.msg, "Internet access blocked");
    assert_eq!(guard.reason.option_key, "filter/blockInternet");
}

#[test]
fn some_survivors_do_not_block() {
    let fx = fx(json!({"filter": {"defaultAction": "permit", "blockLAN": true}}));
    let conn = dns_conn(&fx, "example.com.");
    let rr = response(
        "example.com.",
        IpScope::SiteLocal,
        vec![
            a("example.com.", "192.168.1.9"),
            a("example.com.", "93.184.216.34"),
        ],
    );
    let out = run_filter(&fx, &conn, rr, false);
    assert_eq!(out.answer.len(), 1);
    assert!(out.filtered);
    assert_ne!(conn.lock().unwrap().verdict, Verdict::Block);
}

#[test]
fn out_of_scope_split_view_is_removed() {
    let fx = fx(permit());
    let conn = dns_conn(&fx, "intranet.example.com.");
    // A global resolver must not answer with LAN or localhost addresses.
    let rr = response(
        "intranet.example.com.",
        IpScope::Global,
        vec![a("intranet.example.com.", "192.168.1.9")],
    );
    let out = run_filter(&fx, &conn, rr, false);
    assert!(out.answer.is_empty());
    let guard = conn.lock().unwrap();
    assert_eq!(guard.verdict, Verdict::Block);
    assert_eq!(guard.reason.msg, "DNS global/private split-view violation");
    assert_eq!(guard.reason.option_key, "filter/removeOutOfScopeDNS");

    drop(guard);
    let conn = dns_conn(&fx, "local.example.com.");
    let rr = response(
        "local.example.com.",
        IpScope::SiteLocal,
        vec![a("local.example.com.", "127.0.0.1")],
    );
    let out = run_filter(&fx, &conn, rr, false);
    assert!(out.answer.is_empty());
    assert_eq!(conn.lock().unwrap().verdict, Verdict::Block);
}

#[test]
fn lan_answer_from_lan_resolver_is_kept() {
    let fx = fx(permit());
    let conn = dns_conn(&fx, "nas.example.com.");
    let rr = response(
        "nas.example.com.",
        IpScope::SiteLocal,
        vec![a("nas.example.com.", "192.168.1.9")],
    );
    let out = run_filter(&fx, &conn, rr, false);
    assert_eq!(out.answer.len(), 1);
    assert!(!out.filtered);
}

#[test]
fn system_resolver_only_gets_localhost_filtering() {
    let fx = fx(block_internet());
    let p = &fx.layered;
    let section = filter_dns_section(
        vec![
            a("a.example.", "93.184.216.34"),
            a("a.example.", "192.168.1.9"),
            a("a.example.", "127.0.0.1"),
        ],
        p,
        IpScope::Global,
        true,
    );
    // Blocked scopes and LAN answers are not filtered for the system resolver.
    assert_eq!(section.good.len(), 2);
    assert_eq!(section.allowed_address_records, 2);
    assert_eq!(section.filtered.len(), 1);
}

#[test]
fn own_queries_are_not_filtered() {
    let fx = fx(block_internet());
    let conn = dns_conn(&fx, "example.com.");
    {
        let mut guard = conn.lock().unwrap();
        let mut process = Process::empty("999-1");
        process.pid = OWN_PID;
        guard.process = Some(Arc::new(process));
    }
    let rr = response(
        "example.com.",
        IpScope::Global,
        vec![a("example.com.", "93.184.216.34")],
    );
    let out = run_filter(&fx, &conn, rr, false);
    assert_eq!(out.answer.len(), 1);
    assert!(!out.filtered);
}

#[test]
fn cname_denied_by_endpoint_rule_blocks() {
    let fx = fx(json!({"filter": {"defaultAction": "permit", "endpoints": ["- tracker.example"]}}));
    let conn = dns_conn(&fx, "www.example.com.");
    let mut guard = conn.lock().unwrap();
    guard.entity.as_mut().unwrap().cname = vec!["tracker.example.".to_string()];
    assert!(may_block_cnames(&mut guard, &fx.layered));
    assert_eq!(guard.verdict, Verdict::Block);
    assert_eq!(guard.reason.option_key, "filter/includeCNAMEs");
}

#[test]
fn cname_check_is_skipped_when_disabled() {
    let fx = fx(json!({"filter": {
        "defaultAction": "permit",
        "includeCNAMEs": false,
        "endpoints": ["- tracker.example"],
    }}));
    let conn = dns_conn(&fx, "www.example.com.");
    let mut guard = conn.lock().unwrap();
    guard.entity.as_mut().unwrap().cname = vec!["tracker.example.".to_string()];
    assert!(!may_block_cnames(&mut guard, &fx.layered));
    assert_ne!(guard.verdict, Verdict::Block);
}

#[tokio::test]
async fn hook_blocks_dns_request_for_blocked_domain() {
    let fx = fx(json!({"filter": {"defaultAction": "permit", "endpoints": ["- blocked.example"]}}));
    let conn = dns_conn(&fx, "blocked.example.");
    let outcome = fx.hook.filter_connection(&conn).await;
    assert!(outcome.responder.is_none());
    assert_eq!(conn.lock().unwrap().verdict, Verdict::Block);

    let conn = dns_conn(&fx, "fine.example.org.");
    fx.hook.filter_connection(&conn).await;
    assert_eq!(conn.lock().unwrap().verdict, Verdict::Accept);
}

#[tokio::test]
async fn hook_filters_resolved_dns_and_returns_the_cache() {
    let fx = fx(block_internet());
    let conn = dns_conn(&fx, "example.com.");
    let q = Query::new("example.com.", RecordType::A);
    let rr = response(
        "example.com.",
        IpScope::Global,
        vec![a("example.com.", "93.184.216.34")],
    );
    let (out, outcome) = fx.hook.filter_resolved_dns(&conn, &q, rr).await;
    assert!(outcome.responder.is_none());
    assert!(out.unwrap().answer.is_empty());
    assert_eq!(conn.lock().unwrap().verdict, Verdict::Block);
}

#[tokio::test]
async fn allowed_answer_is_saved_and_found_by_domain_lookup() {
    let fx = fx(permit());
    let conn = dns_conn(&fx, "www.example.com.");
    let q = Query::new("www.example.com.", RecordType::A);
    let rr = response(
        "www.example.com.",
        IpScope::Global,
        vec![
            cname("www.example.com.", "cdn.example.net."),
            a("cdn.example.net.", "93.184.216.34"),
            a("cdn.example.net.", "127.0.0.1"),
        ],
    );
    let (out, _) = fx.hook.filter_resolved_dns(&conn, &q, rr).await;
    let out = out.unwrap();
    fx.hook.update_ips_and_cnames(&q, &out, &conn);

    // The CNAMEs are linked to the DNS request.
    assert_eq!(
        conn.lock().unwrap().entity.as_ref().unwrap().cname,
        ["cdn.example.net."]
    );

    // The profile scoped record exists; the localhost IP is never saved.
    let info = fx.store.get("p1", "93.184.216.34").unwrap();
    assert_eq!(info.resolved_domains.len(), 1);
    assert!(fx.store.get("p1", "127.0.0.1").is_err());

    let domains = IpInfoDomains {
        store: Arc::clone(&fx.store),
    };
    let found = domains
        .most_recent_domain("p1", ip("93.184.216.34"))
        .unwrap()
        .unwrap();
    assert_eq!(found.domain, "www.example.com.");
    assert_eq!(found.cnames, ["cdn.example.net."]);
    assert_eq!(found.dns_context.unwrap().question, "A");
    assert!(domains.most_recent_domain("p1", ip("1.1.1.1")).is_err());
}

#[test]
fn unidentified_profiles_save_globally() {
    use pm_firewall::dns_filter::ip_info_profile_scope;
    assert_eq!(ip_info_profile_scope("_unidentified"), "global");
    assert_eq!(ip_info_profile_scope("_system-resolver"), "global");
    assert_eq!(ip_info_profile_scope("abc"), "abc");
}
