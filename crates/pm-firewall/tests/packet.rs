#![allow(clippy::field_reassign_with_default)]
//! Behavior of the packet handler chain, derived from Go `packet_handler.go`.

use std::collections::HashMap;
use std::net::IpAddr;
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{Arc, Condvar, Mutex};
use std::time::Duration;
use std::time::SystemTime;

use pm_config::Config;
use pm_database::{Databases, MemoryStorage};
use pm_firewall::packet::{
    BandwidthMethod, BandwidthSink, BandwidthUpdate, ConnectionTuple, ConntrackControl, IcmpType,
    LayeredProfileLookup, PacketEnv, PacketHandler, QueuedPacket, VerdictError,
};
use pm_firewall::*;
use pm_intel::{cache_interface, Entity, FilterLists, StaticLocations};
use pm_interception::Verdict as PktVerdict;
use pm_netenv::OnlineStatus;
use pm_network::{
    Clock, Connection, Environment, LocalProfileInfo, ManualClock, Network, PacketInfo,
    ProcessResolver, ProfileLookup, SharedConnection, Verdict,
};
use pm_process::Process;
use pm_profile::options::register_options;
use pm_profile::{GlobalProfileConfig, LayeredProfile, Profile, SOURCE_LOCAL};
use serde_json::{json, Map, Value};

const START: i64 = 1_700_000_000;
const OWN_PID: i32 = 999;

fn ip(s: &str) -> IpAddr {
    s.parse().unwrap()
}

fn process(pid: i32) -> Arc<Process> {
    let mut p = Process::empty(&format!("{pid}-1"));
    p.pid = pid;
    p.name = "firefox".to_string();
    Arc::new(p)
}

#[derive(Default)]
struct FakeResolver {
    pids: Mutex<HashMap<u16, i32>>,
    /// Local ports whose PID lookup blocks until the gate opens.
    gated: Mutex<Vec<u16>>,
    gate_open: Mutex<bool>,
    gate_cv: Condvar,
    entered: AtomicU32,
}

impl FakeResolver {
    fn close_gate(&self, port: u16) {
        *self.gate_open.lock().unwrap() = false;
        self.gated.lock().unwrap().push(port);
    }
    fn open_gate(&self) {
        *self.gate_open.lock().unwrap() = true;
        self.gate_cv.notify_all();
    }
}

impl ProcessResolver for FakeResolver {
    fn pid_of_connection(&self, info: &mut PacketInfo) -> (i32, bool) {
        if self.gated.lock().unwrap().contains(&info.local_port()) {
            self.entered.fetch_add(1, Ordering::SeqCst);
            let mut open = self.gate_open.lock().unwrap();
            while !*open {
                open = self.gate_cv.wait(open).unwrap();
            }
        }
        let pid = self
            .pids
            .lock()
            .unwrap()
            .get(&info.local_port())
            .copied()
            .unwrap_or(-2);
        (pid, info.inbound)
    }
    fn process(&self, pid: i32) -> Arc<Process> {
        process(pid)
    }
    fn network_host(&self, _ip: IpAddr) -> Arc<Process> {
        process(-255)
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
    layered: Mutex<Arc<LayeredProfile>>,
    lookups: AtomicU32,
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
        self.lookups.fetch_add(1, Ordering::SeqCst);
        Some(Arc::clone(&self.layered.lock().unwrap()))
    }
}

#[derive(Default)]
struct TestEnv {
    interception: AtomicBool,
    resolver_disabled: AtomicBool,
    icmp_submitted: AtomicBool,
    my_ips: Vec<IpAddr>,
    api: Option<(IpAddr, u16)>,
    permanent_off: AtomicBool,
    dns_check_domains: Mutex<Vec<String>>,
    cache_updates: Mutex<Vec<(String, usize, i32)>>,
    removed_prompts: AtomicU32,
}

impl FirewallEnv for TestEnv {
    fn own_pid(&self) -> i32 {
        OWN_PID
    }
    fn is_my_ip(&self, ip: IpAddr) -> Result<bool, String> {
        Ok(self.my_ips.contains(&ip))
    }
    fn local_network(&self, _ip: IpAddr) -> Result<Option<LocalNetwork>, String> {
        Ok(None)
    }
    fn multicast_request(&self, _c: &Connection, _n: &LocalNetwork) -> Option<MulticastRequest> {
        None
    }
    fn online_status(&self) -> OnlineStatus {
        OnlineStatus::Online
    }
    fn is_connectivity_domain(&self, _d: &str) -> bool {
        false
    }
    fn resolver_disabled(&self) -> bool {
        self.resolver_disabled.load(Ordering::SeqCst)
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
    fn prompt(&self, _conn: &mut Connection) {}
}

impl PacketEnv for TestEnv {
    fn filter_enabled(&self) -> bool {
        true
    }
    fn permanent_verdicts(&self) -> bool {
        !self.permanent_off.load(Ordering::SeqCst)
    }
    fn dns_query_interception(&self) -> bool {
        self.interception.load(Ordering::SeqCst)
    }
    fn api_endpoint(&self) -> Option<(IpAddr, u16)> {
        self.api
    }
    fn submit_icmp(&self, _pkt: &dyn QueuedPacket) -> bool {
        self.icmp_submitted.load(Ordering::SeqCst)
    }
    fn submit_dns_check_domain(&self, subdomain: &str) {
        self.dns_check_domains
            .lock()
            .unwrap()
            .push(subdomain.to_string());
    }
    fn update_ips_and_cnames(
        &self,
        q: &pm_resolver::Query,
        rr: &pm_resolver::RRCache,
        conn: &Connection,
    ) {
        self.cache_updates
            .lock()
            .unwrap()
            .push((q.fqdn.clone(), rr.answer.len(), conn.pid));
    }
    fn remove_prompt(&self, _conn: &SharedConnection) {
        self.removed_prompts.fetch_add(1, Ordering::SeqCst);
    }
}

#[derive(Default)]
struct FakeConntrack {
    deleted: Mutex<Vec<ConnectionTuple>>,
    all_calls: Mutex<Vec<bool>>,
}

impl ConntrackControl for FakeConntrack {
    fn delete_connection(&self, tuple: &ConnectionTuple) -> std::io::Result<()> {
        self.deleted.lock().unwrap().push(*tuple);
        Ok(())
    }
    fn delete_all_marked(&self, ipv6: bool) -> std::io::Result<usize> {
        self.all_calls.lock().unwrap().push(ipv6);
        Ok(0)
    }
}

type BandwidthCall = (bool, String, String, String, u64, u64);

#[derive(Default)]
struct Sink {
    calls: Mutex<Vec<BandwidthCall>>,
}

impl BandwidthSink for Sink {
    fn update_bandwidth(
        &self,
        history_enabled: bool,
        profile: &str,
        process_key: &str,
        conn_id: &str,
        bytes_received: u64,
        bytes_sent: u64,
    ) -> Result<(), String> {
        self.calls.lock().unwrap().push((
            history_enabled,
            profile.into(),
            process_key.into(),
            conn_id.into(),
            bytes_received,
            bytes_sent,
        ));
        Ok(())
    }
}

#[derive(Clone)]
struct FakePacket {
    info: PacketInfo,
    payload: Vec<u8>,
    icmp: Option<u8>,
    icmp_load_fails: bool,
    fast_tracked: bool,
    info_only: bool,
    seq: u32,
    verdicts: Arc<Mutex<Vec<PktVerdict>>>,
    order: Option<Arc<Mutex<Vec<u32>>>>,
}

/// Dispatches a copy of the packet and waits for the workers; the verdict log
/// is shared with the original.
trait HandlePacket {
    fn handle_packet(&self, pkt: &FakePacket);
}

impl HandlePacket for PacketHandler {
    fn handle_packet(&self, pkt: &FakePacket) {
        self.dispatch(Box::new(pkt.clone()));
        assert!(
            self.wait_idle(Duration::from_secs(10)),
            "workers did not finish"
        );
    }
}

impl FakePacket {
    fn new(
        protocol: u8,
        src: &str,
        src_port: u16,
        dst: &str,
        dst_port: u16,
        inbound: bool,
    ) -> FakePacket {
        FakePacket {
            info: PacketInfo::new(protocol, ip(src), src_port, ip(dst), dst_port, inbound),
            payload: Vec::new(),
            icmp: None,
            icmp_load_fails: false,
            fast_tracked: false,
            info_only: false,
            seq: 0,
            verdicts: Arc::new(Mutex::new(Vec::new())),
            order: None,
        }
    }
    fn tcp(dst: &str, dst_port: u16) -> FakePacket {
        FakePacket::new(6, "192.168.1.5", 43210, dst, dst_port, false)
    }
    fn udp(dst: &str, dst_port: u16) -> FakePacket {
        FakePacket::new(17, "192.168.1.5", 43210, dst, dst_port, false)
    }
    fn verdicts(&self) -> Vec<PktVerdict> {
        self.verdicts.lock().unwrap().clone()
    }
    fn last(&self) -> PktVerdict {
        *self.verdicts().last().expect("no verdict issued")
    }
}

impl QueuedPacket for FakePacket {
    fn info(&self) -> &PacketInfo {
        &self.info
    }
    fn info_only(&self) -> bool {
        self.info_only
    }
    fn verdict_set(&self) -> bool {
        !self.verdicts.lock().unwrap().is_empty()
    }
    fn fast_tracked_by_integration(&self) -> bool {
        self.fast_tracked
    }
    fn payload(&self) -> Option<&[u8]> {
        Some(&self.payload)
    }
    fn raw(&self) -> &[u8] {
        &self.payload
    }
    fn icmp_type(&self) -> IcmpType {
        match (self.icmp_load_fails, self.icmp) {
            (true, _) => IcmpType::LoadFailed,
            (false, Some(t)) => IcmpType::Type(t),
            (false, None) => IcmpType::NoLayer,
        }
    }
    fn set_verdict(&self, verdict: PktVerdict) -> Result<(), VerdictError> {
        self.verdicts.lock().unwrap().push(verdict);
        if let Some(order) = &self.order {
            order.lock().unwrap().push(self.seq);
        }
        Ok(())
    }
}

struct Fx {
    handler: PacketHandler,
    network: Arc<Network>,
    env: Arc<TestEnv>,
    profiles: Arc<Profiles>,
    conntrack: Arc<FakeConntrack>,
    sink: Arc<Sink>,
    resolver: Arc<FakeResolver>,
    preauth: Arc<PreAuthPorts>,
    config: Arc<Config>,
    global: Arc<GlobalProfileConfig>,
    _databases: Arc<Databases>,
}

fn fx_with(env: TestEnv, profile: Value) -> Fx {
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

    let profiles = Arc::new(Profiles {
        layered: Mutex::new(layered(&config, &global, profile)),
        lookups: AtomicU32::new(0),
    });
    let resolver = Arc::new(FakeResolver::default());
    resolver.pids.lock().unwrap().insert(43210, 4242);
    let clock = Arc::new(ManualClock::new(START));
    let network_env = Environment::new(resolver.clone(), factory)
        .with_clock(clock as Arc<dyn Clock>)
        .with_profiles(profiles.clone());
    let network = Network::new(network_env);
    let env = Arc::new(env);
    let conntrack = Arc::new(FakeConntrack::default());
    let sink = Arc::new(Sink::default());
    let preauth = Arc::new(PreAuthPorts::new());
    let handler = PacketHandler::new(
        Arc::clone(&network),
        env.clone(),
        profiles.clone(),
        Arc::clone(&preauth),
        conntrack.clone(),
    )
    .with_bandwidth_sink(sink.clone());
    Fx {
        handler,
        network,
        env,
        profiles,
        conntrack,
        sink,
        resolver,
        preauth,
        config,
        global,
        _databases: databases,
    }
}

fn layered(
    config: &Arc<Config>,
    global: &Arc<GlobalProfileConfig>,
    profile: Value,
) -> Arc<LayeredProfile> {
    let local = Arc::new(Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        id: "p1".to_string(),
        config: profile.as_object().cloned().unwrap_or_else(Map::new),
        ..Profile::default()
    }));
    Arc::new(LayeredProfile::new(local, config, global))
}

fn fx(profile: Value) -> Fx {
    fx_with(TestEnv::default(), profile)
}

fn permit() -> Value {
    json!({"filter": {"defaultAction": "permit"}})
}

fn block_all() -> Value {
    json!({"filter": {"defaultAction": "block"}})
}

fn conn_of(fx: &Fx, pkt: &FakePacket) -> SharedConnection {
    fx.network
        .get_connection(&pkt.info.connection_id())
        .expect("connection exists")
}

fn api_env() -> TestEnv {
    TestEnv {
        api: Some((ip("127.0.0.1"), 817)),
        my_ips: vec![ip("127.0.0.1")],
        ..TestEnv::default()
    }
}

// Fast-track rules.

#[test]
fn fast_track_table() {
    let mut icmp_unreachable = FakePacket::new(1, "10.0.0.2", 0, "10.0.0.3", 0, false);
    icmp_unreachable.icmp = Some(3);
    let cases = vec![
        (
            "blocked marker ip",
            FakePacket::tcp("0.0.0.17", 80),
            PktVerdict::PermanentBlock,
        ),
        (
            "blocked marker ipv6",
            FakePacket::new(6, "2001:db8::1", 43210, "::17", 80, false),
            PktVerdict::PermanentBlock,
        ),
        (
            "network self check",
            FakePacket::new(6, "10.0.0.1", 5000, "10.0.0.1", 5000, false),
            PktVerdict::PermanentAccept,
        ),
        (
            "dhcp in lan",
            FakePacket::new(17, "192.168.1.5", 68, "255.255.255.255", 67, false),
            PktVerdict::PermanentAccept,
        ),
        (
            "dhcpv6 link local multicast",
            FakePacket::new(17, "fe80::1", 546, "ff02::1:2", 547, false),
            PktVerdict::PermanentAccept,
        ),
        (
            "api connection",
            FakePacket::new(6, "127.0.0.1", 50000, "127.0.0.1", 817, false),
            PktVerdict::PermanentAccept,
        ),
        (
            "api outbound packet",
            FakePacket::new(6, "127.0.0.1", 817, "127.0.0.1", 50000, false),
            PktVerdict::PermanentAccept,
        ),
        (
            "icmp destination unreachable",
            icmp_unreachable,
            PktVerdict::PermanentAccept,
        ),
        (
            "icmp without readable header",
            FakePacket::new(1, "10.0.0.2", 0, "10.0.0.3", 0, false),
            PktVerdict::PermanentAccept,
        ),
        (
            "system integration check",
            FakePacket::new(61, "127.0.0.1", 0, "127.65.67.75", 0, false),
            PktVerdict::Drop,
        ),
    ];
    for (name, pkt, expect) in cases {
        let fx = fx_with(api_env(), block_all());
        fx.handler.handle_packet(&pkt);
        assert_eq!(pkt.last(), expect, "{name}");
        assert_eq!(pkt.verdicts().len(), 1, "{name}");
    }
}

#[test]
fn integration_fast_tracked_packets_get_no_verdict_from_us() {
    let fx = fx(block_all());
    let mut pkt = FakePacket::tcp("1.1.1.1", 443);
    pkt.fast_tracked = true;
    fx.handler.handle_packet(&pkt);
    assert!(pkt.verdicts().is_empty());
}

#[test]
fn fast_track_negative_cases_continue_to_the_filter() {
    let mut echo = FakePacket::new(1, "192.168.1.5", 0, "10.0.0.3", 0, false);
    echo.icmp = Some(8);
    let negatives = vec![
        // DHCP port to a global address.
        FakePacket::new(17, "192.168.1.5", 43210, "8.8.8.8", 67, false),
        // DHCP ports over TCP.
        FakePacket::new(6, "192.168.1.5", 43210, "192.168.1.1", 67, false),
        // API port on another IP.
        FakePacket::new(6, "192.168.1.5", 43210, "10.1.1.1", 817, false),
        // API port over UDP.
        FakePacket::new(17, "192.168.1.5", 43210, "127.0.0.1", 817, false),
        // Nameserver matcher not set: port 53 is not fast-tracked.
        FakePacket::new(17, "192.168.1.5", 43210, "127.0.0.1", 53, false),
        // Echo request is regular ICMP traffic.
        echo,
    ];
    for pkt in negatives {
        let fx = fx_with(api_env(), permit());
        fx.resolver.pids.lock().unwrap().insert(0, 4242);
        fx.handler.handle_packet(&pkt);
        let shared = conn_of(&fx, &pkt);
        let conn = shared.lock().unwrap();
        assert_eq!(conn.process_pid(), 4242, "{:?}", pkt.info);
        assert_ne!(conn.reason.msg, "fast-tracked", "{:?}", pkt.info);
    }
}

#[test]
fn api_port_from_a_foreign_source_is_not_fast_tracked() {
    let fx = fx_with(api_env(), permit());
    let pkt = FakePacket::new(6, "192.168.1.5", 43210, "127.0.0.1", 817, false);
    fx.handler.handle_packet(&pkt);
    let shared = conn_of(&fx, &pkt);
    assert_ne!(shared.lock().unwrap().reason.msg, "fast-tracked");
}

#[test]
fn local_dns_fast_track_is_permanent_only_with_known_pid() {
    let fx = fx_with(api_env(), permit());
    fx.handler
        .set_nameserver_ip_matcher(Box::new(|ip| ip == "127.0.0.1".parse::<IpAddr>().unwrap()));

    // Known PID from the packet: permanent, and saved for attribution.
    let mut pkt = FakePacket::new(17, "127.0.0.1", 50000, "127.0.0.1", 53, false);
    pkt.info.pid = 4242;
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::PermanentAccept);
    assert!(fx.network.get_dns_request_connection(&pkt.info).is_some());

    // Unknown PID: accepted, but not permanent.
    let pkt = FakePacket::new(17, "127.0.0.1", 50001, "127.0.0.1", 53, false);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::Accept);
}

// Handler chain.

#[test]
fn allowed_outbound_tcp_is_permanently_accepted() {
    let fx = fx(permit());
    let pkt = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::PermanentAccept]);

    let shared = conn_of(&fx, &pkt);
    let conn = shared.lock().unwrap();
    assert_eq!(conn.verdict, Verdict::Accept);
    assert!(conn.verdict_permanent);
    assert!(conn.encrypted);
    assert_eq!(conn.reason.msg, "allowed by default action");
    assert!(conn.key.is_some(), "connection was saved");
}

#[test]
fn blocked_outbound_tcp_is_permanently_blocked() {
    let fx = fx(block_all());
    let pkt = FakePacket::tcp("93.184.216.34", 80);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::PermanentBlock]);
    assert_eq!(fx.handler.stats().blocked, 1);

    let shared = conn_of(&fx, &pkt);
    assert_eq!(shared.lock().unwrap().verdict, Verdict::Block);
}

#[test]
fn inbound_deny_is_a_permanent_drop() {
    let fx = fx(block_all());
    let pkt = FakePacket::new(6, "8.8.8.8", 5000, "192.168.1.5", 43210, true);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::PermanentDrop);
}

#[test]
fn permanent_verdicts_can_be_disabled() {
    let env = TestEnv::default();
    env.permanent_off.store(true, Ordering::SeqCst);
    let fx = fx_with(env, permit());
    let pkt = FakePacket::tcp("93.184.216.34", 80);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::Accept);
}

#[test]
fn second_packet_uses_the_stored_verdict_without_deciding() {
    let fx = fx(permit());
    let first = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&first);
    let lookups = fx.profiles.lookups.load(Ordering::SeqCst);
    assert_eq!(lookups, 1);

    let second = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&second);
    assert_eq!(second.last(), PktVerdict::PermanentAccept);
    assert_eq!(fx.profiles.lookups.load(Ordering::SeqCst), lookups);
}

#[test]
fn verdict_is_never_weakened_by_a_later_packet() {
    let fx = fx(block_all());
    let first = FakePacket::tcp("93.184.216.34", 80);
    fx.handler.handle_packet(&first);
    let second = FakePacket::tcp("93.184.216.34", 80);
    fx.handler.handle_packet(&second);
    assert_eq!(second.last(), PktVerdict::PermanentBlock);
}

#[test]
fn packet_with_verdict_is_ignored() {
    let fx = fx(permit());
    let pkt = FakePacket::tcp("93.184.216.34", 80);
    pkt.set_verdict(PktVerdict::Drop).unwrap();
    fx.handler.handle_packet(&pkt);
    assert!(fx
        .network
        .get_connection(&pkt.info.connection_id())
        .is_none());
    assert_eq!(pkt.verdicts().len(), 1);
}

#[test]
fn info_only_packets_do_not_create_connections() {
    let fx = fx(permit());
    let mut pkt = FakePacket::tcp("93.184.216.34", 80);
    pkt.info_only = true;
    pkt.info.pid = 4242;
    fx.handler.handle_packet(&pkt);
    assert!(fx
        .network
        .get_connection(&pkt.info.connection_id())
        .is_none());
    assert!(pkt.verdicts().is_empty());

    // The reported PID is used by the first real packet.
    fx.resolver.pids.lock().unwrap().clear();
    let real = FakePacket::tcp("93.184.216.34", 80);
    fx.handler.handle_packet(&real);
    assert_eq!(conn_of(&fx, &real).lock().unwrap().pid, 4242);
}

#[test]
fn deciders_get_the_layered_profile_of_the_shared_source() {
    let fx = fx(json!({"filter": {"endpoints": ["- 93.184.216.34"]}}));
    let pkt = FakePacket::tcp("93.184.216.34", 80);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::PermanentBlock);
    let shared = conn_of(&fx, &pkt);
    let conn = shared.lock().unwrap();
    assert_eq!(conn.reason.profile, "local/p1");
    assert_eq!(conn.reason.option_key, "filter/endpoints");
}

#[test]
fn preauthenticated_local_port_is_granted_once() {
    let fx = fx(block_all());
    fx.preauth.register(6, 43210);
    let pkt = FakePacket::tcp("93.184.216.34", 80);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::PermanentAccept);
    let shared = conn_of(&fx, &pkt);
    let conn = shared.lock().unwrap();
    assert!(conn.internal);
    assert_eq!(conn.reason.msg, "connection by Portmaster");
    drop(conn);
    // The registration is consumed.
    assert!(!fx.preauth.is_pre_authenticated(6, 43210));
}

#[test]
fn preauth_is_not_used_for_inbound_connections() {
    let fx = fx(block_all());
    fx.preauth.register(6, 43210);
    let pkt = FakePacket::new(6, "8.8.8.8", 5000, "192.168.1.5", 43210, true);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::PermanentDrop);
}

#[test]
fn own_permanent_outbound_connections_are_finalized() {
    let fx = fx(permit());
    fx.resolver.pids.lock().unwrap().insert(43210, OWN_PID);
    let pkt = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::PermanentAcceptFinal);
}

#[test]
fn rogue_dns_queries_are_redirected_to_the_nameserver() {
    let env = TestEnv::default();
    env.interception.store(true, Ordering::SeqCst);
    let fx = fx_with(env, block_all());
    fx.handler
        .set_nameserver_ip_matcher(Box::new(|ip| ip == "127.0.0.1".parse::<IpAddr>().unwrap()));
    let pkt = FakePacket::udp("8.8.8.8", 53);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::RerouteToNameserver);
    let shared = conn_of(&fx, &pkt);
    let conn = shared.lock().unwrap();
    assert_eq!(conn.verdict, Verdict::RerouteToNameserver);
    assert!(conn.internal);
    drop(conn);
    assert!(fx.network.get_dns_request_connection(&pkt.info).is_some());

    // Later packets keep being rerouted by the stored verdict.
    let again = FakePacket::udp("8.8.8.8", 53);
    fx.handler.handle_packet(&again);
    assert_eq!(again.last(), PktVerdict::RerouteToNameserver);
}

#[test]
fn dns_to_the_nameserver_itself_is_not_redirected() {
    let env = TestEnv::default();
    env.interception.store(true, Ordering::SeqCst);
    let fx = fx_with(env, permit());
    fx.handler
        .set_nameserver_ip_matcher(Box::new(|ip| ip == "8.8.8.8".parse::<IpAddr>().unwrap()));
    let pkt = FakePacket::udp("8.8.8.8", 53);
    fx.handler.handle_packet(&pkt);
    assert_ne!(pkt.last(), PktVerdict::RerouteToNameserver);
}

// DNS inspection.

fn dns_response(name: &str, answers: usize) -> Vec<u8> {
    use hickory_proto::op::{Message, MessageType, Query};
    use hickory_proto::rr::rdata::A;
    use hickory_proto::rr::{Name, RData, Record, RecordType};
    let name = Name::from_ascii(name).unwrap();
    let mut message = Message::query();
    message.metadata.message_type = MessageType::Response;
    message.add_query(Query::query(name.clone(), RecordType::A));
    for i in 0..answers {
        message.add_answer(Record::from_rdata(
            name.clone(),
            60,
            RData::A(A::new(1, 2, 3, i as u8)),
        ));
    }
    message.to_vec().unwrap()
}

/// With the resolver disabled, outgoing DNS is allowed and inspected.
fn inspecting_fx() -> Fx {
    let env = TestEnv::default();
    env.resolver_disabled.store(true, Ordering::SeqCst);
    fx_with(env, permit())
}

fn dns_packet(payload: Vec<u8>) -> FakePacket {
    let mut pkt = FakePacket::udp("8.8.8.8", 53);
    pkt.payload = payload;
    pkt
}

#[test]
fn inspected_dns_responses_update_the_cache_and_are_accepted() {
    let fx = inspecting_fx();
    let pkt = dns_packet(dns_response("example.com.", 2));
    fx.handler.handle_packet(&pkt);
    assert!(conn_of(&fx, &pkt).lock().unwrap().inspecting);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::Accept]);
    let updates = fx.env.cache_updates.lock().unwrap().clone();
    assert_eq!(updates, vec![("example.com.".to_string(), 2, 4242)]);

    // The connection stays in the DNS inspection handler.
    let next = dns_packet(dns_response("example.org.", 1));
    fx.handler.handle_packet(&next);
    assert_eq!(next.verdicts(), vec![PktVerdict::Accept]);
    assert_eq!(fx.env.cache_updates.lock().unwrap().len(), 2);
}

#[test]
fn dns_packet_without_dns_data_is_blocked_permanently() {
    let fx = inspecting_fx();
    let pkt = dns_packet(vec![1, 2, 3]);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::PermanentBlock]);
    let shared = conn_of(&fx, &pkt);
    let conn = shared.lock().unwrap();
    assert_eq!(conn.verdict, Verdict::Block);
    assert!(conn.verdict_permanent);
    assert_eq!(conn.reason.msg, "none DNS data on DNS port");
}

#[test]
fn dns_self_check_domain_is_submitted_and_not_cached() {
    let fx = inspecting_fx();
    let pkt = dns_packet(dns_response("abc123.self-check.portmaster.home.arpa.", 1));
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::Accept);
    assert_eq!(
        fx.env.dns_check_domains.lock().unwrap().clone(),
        vec!["abc123".to_string()]
    );
    assert!(fx.env.cache_updates.lock().unwrap().is_empty());
}

#[test]
fn dns_response_without_answer_is_accepted() {
    let fx = inspecting_fx();
    let pkt = dns_packet(dns_response("example.com.", 0));
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.last(), PktVerdict::Accept);
    assert!(fx.env.cache_updates.lock().unwrap().is_empty());
}

#[test]
fn handler_state_is_pruned_with_the_connection() {
    let fx = inspecting_fx();
    let pkt = dns_packet(dns_response("example.com.", 1));
    fx.handler.handle_packet(&pkt);
    assert_eq!(fx.handler.tracked_handlers(), 1);
    let shared = conn_of(&fx, &pkt);
    fx.network.delete_locked(&mut shared.lock().unwrap());
    fx.handler.prune_handlers();
    assert_eq!(fx.handler.tracked_handlers(), 0);
}

// ICMP.

fn icmp(src: &str, dst: &str, kind: u8, inbound: bool) -> FakePacket {
    let mut p = FakePacket::new(1, src, 0, dst, 0, inbound);
    p.icmp = Some(kind);
    p
}

#[test]
fn icmp_echo_is_filtered_without_permanent_verdict() {
    let fx = fx(permit());
    fx.resolver.pids.lock().unwrap().insert(0, 4242);
    let pkt = icmp("192.168.1.5", "8.8.8.8", 8, false);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::Accept]);

    // The next echo packet is handled by the ICMP handler again.
    let again = icmp("192.168.1.5", "8.8.8.8", 8, false);
    fx.handler.handle_packet(&again);
    assert_eq!(again.verdicts(), vec![PktVerdict::Accept]);
    assert!(!conn_of(&fx, &again).lock().unwrap().verdict_permanent);
}

#[test]
fn icmp_echo_to_a_blocked_destination_is_blocked() {
    let fx = fx(block_all());
    fx.resolver.pids.lock().unwrap().insert(0, 4242);
    let pkt = icmp("192.168.1.5", "8.8.8.8", 8, false);
    fx.handler.handle_packet(&pkt);
    // The mark mapping turns a blocked ICMPv4 packet into a drop.
    assert_eq!(pkt.last(), PktVerdict::Block);
    assert_eq!(conn_of(&fx, &pkt).lock().unwrap().verdict, Verdict::Block);
}

#[test]
fn icmp_listener_consumes_echo_packets() {
    let env = TestEnv::default();
    env.icmp_submitted.store(true, Ordering::SeqCst);
    let fx = fx_with(env, permit());
    fx.resolver.pids.lock().unwrap().insert(0, 4242);
    // Fast track submits and accepts without a permanent verdict.
    let pkt = icmp("192.168.1.5", "8.8.8.8", 8, false);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::Accept]);
}

#[test]
fn inbound_icmp_turns_outbound_on_first_outbound_packet() {
    let fx = fx(permit());
    fx.resolver.pids.lock().unwrap().insert(0, 4242);
    let first = icmp("8.8.8.8", "192.168.1.5", 8, true);
    fx.handler.handle_packet(&first);
    let shared = conn_of(&fx, &first);
    assert!(shared.lock().unwrap().inbound);

    // Same connection ID, now seen in the outbound direction.
    let second = icmp("192.168.1.5", "8.8.8.8", 8, false);
    assert_eq!(second.info.connection_id(), first.info.connection_id());
    fx.handler.handle_packet(&second);
    assert!(!shared.lock().unwrap().inbound);
    assert_eq!(second.verdicts().len(), 1);
}

#[test]
fn icmp_packet_that_fails_to_load_is_dropped() {
    let fx = fx(permit());
    fx.resolver.pids.lock().unwrap().insert(0, 4242);
    fx.handler
        .handle_packet(&icmp("192.168.1.5", "8.8.8.8", 8, false));
    let mut pkt = FakePacket::new(1, "192.168.1.5", 0, "8.8.8.8", 0, false);
    pkt.icmp_load_fails = true;
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::Drop]);
}

#[test]
fn other_icmp_types_of_a_tracked_connection_are_accepted() {
    let fx = fx(permit());
    fx.resolver.pids.lock().unwrap().insert(0, 4242);
    fx.handler
        .handle_packet(&icmp("192.168.1.5", "8.8.8.8", 8, false));
    let pkt = icmp("192.168.1.5", "8.8.8.8", 3, false);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::Accept]);
}

// Bandwidth.

fn update(id: &str, received: u64, sent: u64, method: BandwidthMethod) -> BandwidthUpdate {
    BandwidthUpdate {
        conn_id: id.to_string(),
        bytes_received: received,
        bytes_sent: sent,
        method,
    }
}

#[test]
fn bandwidth_updates_follow_the_method() {
    let fx = fx(permit());
    let pkt = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&pkt);
    let id = pkt.info.connection_id();

    fx.handler
        .update_bandwidth(&update(&id, 100, 10, BandwidthMethod::Absolute));
    fx.handler
        .update_bandwidth(&update(&id, 150, 30, BandwidthMethod::Absolute));
    fx.handler
        .update_bandwidth(&update(&id, 5, 1, BandwidthMethod::Additive));
    // Empty updates and unknown connections are ignored.
    fx.handler
        .update_bandwidth(&update(&id, 0, 0, BandwidthMethod::Additive));
    fx.handler
        .update_bandwidth(&update("nope", 1, 1, BandwidthMethod::Additive));

    let shared = fx.network.get_connection(&id).unwrap();
    let conn = shared.lock().unwrap();
    assert_eq!((conn.bytes_received, conn.bytes_sent), (155, 31));
    drop(conn);
    let calls = fx.sink.calls.lock().unwrap().clone();
    let deltas: Vec<(u64, u64)> = calls.iter().map(|c| (c.4, c.5)).collect();
    assert_eq!(deltas, vec![(100, 10), (50, 20), (5, 1)]);
    assert_eq!(calls[0].1, "local/p1");
    assert_eq!(calls[0].2, "4242-1");
    assert_eq!(calls[0].3, id);
}

#[test]
fn bandwidth_update_skips_locked_connections() {
    let fx = fx(permit());
    let pkt = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&pkt);
    let shared = conn_of(&fx, &pkt);
    let guard = shared.lock().unwrap();
    fx.handler.update_bandwidth(&update(
        &pkt.info.connection_id(),
        1,
        1,
        BandwidthMethod::Additive,
    ));
    drop(guard);
    assert_eq!(shared.lock().unwrap().bytes_received, 0);
    assert!(fx.sink.calls.lock().unwrap().is_empty());
}

// Verdict reset.

fn switch_profile(fx: &Fx, config: Value) {
    *fx.profiles.layered.lock().unwrap() = layered(&fx.config, &fx.global, config);
}

#[test]
fn reset_single_connection_reevaluates_and_clears_conntrack_on_change() {
    let fx = fx(permit());
    let pkt = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&pkt);
    let id = pkt.info.connection_id();

    // Unchanged verdict: no conntrack deletion.
    fx.handler.reset_single_connection_verdict(&id);
    assert!(fx.conntrack.deleted.lock().unwrap().is_empty());
    assert_eq!(fx.env.removed_prompts.load(Ordering::SeqCst), 1);

    switch_profile(&fx, block_all());
    fx.handler.reset_single_connection_verdict(&id);
    let deleted = fx.conntrack.deleted.lock().unwrap().clone();
    assert_eq!(
        deleted,
        vec![ConnectionTuple {
            src: ip("192.168.1.5"),
            dst: ip("93.184.216.34"),
            protocol: 6,
            src_port: 43210,
            dst_port: 443,
        }]
    );
    let shared = fx.network.get_connection(&id).unwrap();
    assert_eq!(shared.lock().unwrap().verdict, Verdict::Block);

    // Unknown connections are ignored.
    fx.handler.reset_single_connection_verdict("missing");
}

#[test]
fn reset_by_profile_only_touches_matching_connections() {
    let fx = fx(permit());
    let a = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&a);
    fx.resolver.pids.lock().unwrap().insert(43211, 4242);
    let mut b = FakePacket::tcp("93.184.216.35", 443);
    b.info.src_port = 43211;
    fx.handler.handle_packet(&b);
    // Make the second connection belong to another profile.
    conn_of(&fx, &b).lock().unwrap().process_context.profile = "other".into();

    switch_profile(&fx, block_all());
    fx.handler
        .reset_profile_connection_verdict(SOURCE_LOCAL, "p1");
    assert_eq!(conn_of(&fx, &a).lock().unwrap().verdict, Verdict::Block);
    assert_eq!(conn_of(&fx, &b).lock().unwrap().verdict, Verdict::Accept);
    assert_eq!(fx.conntrack.deleted.lock().unwrap().len(), 1);
}

#[test]
fn reset_all_skips_ended_internal_and_incomplete_connections() {
    let fx = fx(permit());
    let a = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&a);
    fx.resolver.pids.lock().unwrap().insert(43211, 4242);
    let mut ended = FakePacket::tcp("93.184.216.35", 443);
    ended.info.src_port = 43211;
    fx.handler.handle_packet(&ended);
    conn_of(&fx, &ended).lock().unwrap().ended = START;
    fx.resolver.pids.lock().unwrap().insert(43212, 4242);
    let mut internal = FakePacket::tcp("93.184.216.36", 443);
    internal.info.src_port = 43212;
    fx.handler.handle_packet(&internal);
    conn_of(&fx, &internal).lock().unwrap().internal = true;
    // An incomplete connection (no real packet yet).
    let incomplete = PacketInfo::new(6, ip("192.168.1.5"), 1, ip("1.1.1.1"), 2, false);
    fx.network.new_incomplete_connection(&incomplete);

    switch_profile(&fx, block_all());
    fx.handler.reset_all_connection_verdicts();
    assert_eq!(conn_of(&fx, &a).lock().unwrap().verdict, Verdict::Block);
    assert_eq!(
        conn_of(&fx, &ended).lock().unwrap().verdict,
        Verdict::Accept
    );
    assert_eq!(
        conn_of(&fx, &internal).lock().unwrap().verdict,
        Verdict::Accept
    );
    assert_eq!(fx.conntrack.deleted.lock().unwrap().len(), 1);
}

#[test]
fn conntrack_wide_reset_uses_the_ipv6_setting() {
    let fx = fx(permit());
    fx.handler.reset_all_marked_conntrack().unwrap();
    assert_eq!(fx.conntrack.all_calls.lock().unwrap().clone(), vec![true]);
}

#[test]
fn icmp_without_layer_continues_as_echo_like_go() {
    let fx = fx(permit());
    fx.resolver.pids.lock().unwrap().insert(0, 4242);
    fx.handler
        .handle_packet(&icmp("192.168.1.5", "8.8.8.8", 8, false));
    let pkt = FakePacket::new(1, "192.168.1.5", 0, "8.8.8.8", 0, false);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::Accept]);

    // With a listener, the fast track still submits it first.
    let env = TestEnv::default();
    env.icmp_submitted.store(true, Ordering::SeqCst);
    let fx = fx_with(env, permit());
    let pkt = FakePacket::new(1, "192.168.1.5", 0, "8.8.8.8", 0, false);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::Accept]);
}

// Dispatch model.

fn wait_for(what: &str, cond: impl Fn() -> bool) {
    let deadline = std::time::Instant::now() + Duration::from_secs(10);
    while !cond() {
        assert!(std::time::Instant::now() < deadline, "timeout: {what}");
        std::thread::sleep(Duration::from_millis(1));
    }
}

#[test]
fn back_to_back_first_packets_both_reach_the_decision() {
    let fx = fx(permit());
    fx.resolver.close_gate(43210);
    let a = FakePacket::tcp("93.184.216.34", 443);
    let b = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.dispatch(Box::new(a.clone()));
    // The first packet is stuck gathering data; the second arrives meanwhile.
    wait_for("first worker", || {
        fx.resolver.entered.load(Ordering::SeqCst) == 1
    });
    fx.handler.dispatch(Box::new(b.clone()));
    assert!(a.verdicts().is_empty() && b.verdicts().is_empty());
    fx.resolver.open_gate();
    assert!(fx.handler.wait_idle(Duration::from_secs(10)));

    assert_eq!(a.verdicts(), vec![PktVerdict::PermanentAccept]);
    assert_eq!(b.verdicts(), vec![PktVerdict::PermanentAccept]);
    assert_eq!(fx.profiles.lookups.load(Ordering::SeqCst), 1);
    assert_eq!(fx.handler.tracked_handlers(), 0);
}

#[test]
fn packets_of_a_connection_keep_their_order() {
    let fx = fx(permit());
    fx.resolver.close_gate(43210);
    let order = Arc::new(Mutex::new(Vec::new()));
    let packets: Vec<FakePacket> = (0..20)
        .map(|seq| {
            let mut p = FakePacket::tcp("93.184.216.34", 443);
            p.seq = seq;
            p.order = Some(Arc::clone(&order));
            p
        })
        .collect();
    fx.handler.dispatch(Box::new(packets[0].clone()));
    wait_for("worker", || fx.resolver.entered.load(Ordering::SeqCst) == 1);
    for p in &packets[1..] {
        fx.handler.dispatch(Box::new(p.clone()));
    }
    fx.resolver.open_gate();
    assert!(fx.handler.wait_idle(Duration::from_secs(10)));
    assert_eq!(*order.lock().unwrap(), (0..20).collect::<Vec<u32>>());
}

#[test]
fn a_slow_connection_does_not_stall_others() {
    let fx = fx(permit());
    fx.resolver.pids.lock().unwrap().insert(43211, 4242);
    fx.resolver.close_gate(43210);
    let slow = FakePacket::tcp("93.184.216.34", 443);
    let mut fast = FakePacket::tcp("93.184.216.35", 443);
    fast.info.src_port = 43211;
    fx.handler.dispatch(Box::new(slow.clone()));
    wait_for("slow worker", || {
        fx.resolver.entered.load(Ordering::SeqCst) == 1
    });
    fx.handler.dispatch(Box::new(fast.clone()));
    wait_for("fast verdict", || !fast.verdicts().is_empty());
    assert!(slow.verdicts().is_empty());
    fx.resolver.open_gate();
    assert!(fx.handler.wait_idle(Duration::from_secs(10)));
    assert_eq!(slow.last(), PktVerdict::PermanentAccept);
}

#[test]
fn a_full_queue_drops_the_packet() {
    let fx = fx(permit());
    fx.resolver.close_gate(43210);
    fx.handler
        .dispatch(Box::new(FakePacket::tcp("93.184.216.34", 443)));
    wait_for("worker", || fx.resolver.entered.load(Ordering::SeqCst) == 1);
    let queued: Vec<FakePacket> = (0..100)
        .map(|_| FakePacket::tcp("93.184.216.34", 443))
        .collect();
    for p in &queued {
        fx.handler.dispatch(Box::new(p.clone()));
    }
    let overflow = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.dispatch(Box::new(overflow.clone()));
    // Dropped right away, without a permanent mark.
    assert_eq!(overflow.verdicts(), vec![PktVerdict::Drop]);
    fx.resolver.open_gate();
    assert!(fx.handler.wait_idle(Duration::from_secs(10)));
    assert!(queued
        .iter()
        .all(|p| p.last() == PktVerdict::PermanentAccept));
}

#[test]
fn shutdown_discards_queued_packets() {
    let fx = fx(permit());
    fx.resolver.close_gate(43210);
    fx.handler
        .dispatch(Box::new(FakePacket::tcp("93.184.216.34", 443)));
    wait_for("worker", || fx.resolver.entered.load(Ordering::SeqCst) == 1);
    let queued = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.dispatch(Box::new(queued.clone()));
    fx.handler.shutdown();
    fx.resolver.open_gate();
    assert!(fx.handler.wait_idle(Duration::from_secs(10)));
    assert!(queued.verdicts().is_empty());
    // Nothing is handled after shutdown.
    let late = FakePacket::tcp("93.184.216.36", 443);
    fx.handler.dispatch(Box::new(late.clone()));
    assert!(late.verdicts().is_empty());
}

#[test]
fn connection_without_state_that_is_incomplete_is_not_dropped() {
    let fx = fx(permit());
    // Created behind the dispatcher's back: no handler state.
    let info = PacketInfo::new(6, ip("192.168.1.5"), 43210, ip("93.184.216.34"), 443, false);
    fx.network.new_incomplete_connection(&info);
    let pkt = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&pkt);
    assert_eq!(pkt.verdicts(), vec![PktVerdict::PermanentAccept]);
}

#[test]
fn packet_after_a_reset_gets_the_new_permanent_mark() {
    let fx = fx(permit());
    let first = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&first);
    assert_eq!(first.last(), PktVerdict::PermanentAccept);

    switch_profile(&fx, block_all());
    fx.handler
        .reset_single_connection_verdict(&first.info.connection_id());

    // The conntrack entry is gone; the next packet is marked from the new verdict.
    let next = FakePacket::tcp("93.184.216.34", 443);
    fx.handler.handle_packet(&next);
    assert_eq!(next.verdicts(), vec![PktVerdict::PermanentBlock]);
}
