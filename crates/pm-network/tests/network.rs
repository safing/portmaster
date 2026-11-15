//! Behavior of the connection constructors, the store, the cleaner, DNS
//! bookkeeping and the `network` database.

mod common;

use std::net::IpAddr;
use std::sync::Arc;

use common::{ip, setup, Setup, START};
use pm_database::Databases;
use pm_network::Clock;
use pm_network::{
    parse_db_key, register_as_database, ConnectionType, DomainLookup, Environment,
    LocalProfileInfo, Network, OpenDnsQuery, PacketInfo, ProfileLookup, ResolvedDomain,
    SharedConnection, Verdict, DNS_TYPE_A, DNS_TYPE_SVCB,
};
use pm_process::Process;
use pm_query::Query;

fn packet(
    protocol: u8,
    src: &str,
    src_port: u16,
    dst: &str,
    dst_port: u16,
    inbound: bool,
) -> PacketInfo {
    let mut info = PacketInfo::new(protocol, ip(src), src_port, ip(dst), dst_port, inbound);
    info.seen_at = std::time::UNIX_EPOCH + std::time::Duration::from_secs(START as u64);
    info
}

fn outbound_tcp() -> PacketInfo {
    packet(6, "192.168.1.5", 43210, "93.184.216.34", 443, false)
}

fn with_network() -> (Setup, Arc<Databases>) {
    let setup = setup();
    setup.resolver.add_process(4242, "firefox");
    setup.resolver.pids.lock().unwrap().insert(43210, 4242);
    let databases = Arc::new(Databases::without_disk());
    register_as_database(&setup.network, &databases).unwrap();
    (setup, databases)
}

fn query(prefix: &str) -> Query {
    Query::new(prefix).check().unwrap()
}

fn keys(records: &[pm_record::Record]) -> Vec<String> {
    let mut keys: Vec<String> = records.iter().map(|r| r.key.to_string()).collect();
    keys.sort();
    keys
}

#[test]
fn incomplete_connection_is_stored_but_not_exposed() {
    let (setup, databases) = with_network();
    let shared = setup.network.new_incomplete_connection(&outbound_tcp());

    let id = "6-192.168.1.5-43210-93.184.216.34-443";
    {
        let conn = shared.lock().unwrap();
        assert_eq!(conn.id, id);
        assert_eq!(conn.conn_type, ConnectionType::Ip);
        assert_eq!(conn.started, START);
        assert!(!conn.data_complete);
        assert!(conn.entity.is_none());
    }
    assert!(setup.network.get_connection(id).is_some());

    let interface = databases.internal_interface();
    let all = keys(&interface.query(&query("network:tree/")).unwrap());
    assert_eq!(
        all,
        vec!["network:tree/4242-1".to_string()],
        "only the process"
    );
}

#[test]
fn too_old_start_is_replaced_by_now() {
    let setup = setup();
    let mut info = outbound_tcp();
    info.seen_at = std::time::UNIX_EPOCH + std::time::Duration::from_secs(1000);
    let shared = setup.network.new_incomplete_connection(&info);
    assert_eq!(shared.lock().unwrap().started, START);
}

#[test]
fn first_packet_gathers_process_scope_and_entity() {
    let (setup, _databases) = with_network();
    let shared = setup
        .network
        .new_connection_from_first_packet(&outbound_tcp());
    let conn = shared.lock().unwrap();

    assert_eq!(conn.pid, 4242);
    assert!(!conn.inbound);
    assert_eq!(conn.scope, "PI");
    assert_eq!(conn.local_ip, Some(ip("192.168.1.5")));
    assert_eq!(conn.local_port, 43210);
    assert_eq!(conn.process_context.process_name, "firefox");
    assert_eq!(conn.process_context.pid, 4242);
    let entity = conn.entity.as_ref().unwrap();
    assert_eq!(entity.ip, Some(ip("93.184.216.34")));
    assert_eq!(entity.port, 443);
    assert_eq!(entity.protocol, 6);
    assert_eq!(entity.dst_port(), 443);
    assert!(conn.data_complete);
    assert!(conn.save_when_finished);
}

#[test]
fn inbound_scopes_follow_the_remote_scope() {
    let setup = setup();
    for (src, expected) in [
        ("127.0.0.1", "IH"),
        ("192.168.1.9", "IL"),
        ("8.8.8.8", "II"),
        ("0.1.2.3", "IX"),
    ] {
        let info = packet(6, src, 5000, "192.168.1.5", 22, true);
        let shared = setup.network.new_connection_from_first_packet(&info);
        let conn = shared.lock().unwrap();
        assert_eq!(conn.scope, expected, "{src}");
        assert!(conn.inbound);
        assert_eq!(conn.local_port, 22);
    }
}

#[test]
fn info_only_packet_does_not_complete_the_connection() {
    let (setup, _) = with_network();
    let shared = setup.network.new_incomplete_connection(&outbound_tcp());
    let mut conn = shared.lock().unwrap();
    setup
        .network
        .gather_connection_info(&mut conn, &outbound_tcp(), true);
    assert!(conn.entity.is_some());
    assert!(conn.process.is_none());
    assert!(!conn.data_complete);
}

#[test]
fn unidentified_connection_uses_fallback_process() {
    let (setup, _) = with_network();
    let info = packet(6, "192.168.1.5", 50000, "1.1.1.1", 443, false);
    let shared = setup.network.new_connection_from_first_packet(&info);
    let conn = shared.lock().unwrap();
    assert_eq!(conn.pid, -2);
    assert_eq!(conn.process_context.process_name, "Other Connections");
}

struct Domains;

impl DomainLookup for Domains {
    fn most_recent_domain(&self, scope: &str, ip: IpAddr) -> Option<ResolvedDomain> {
        // Only the global scope knows the domain.
        (scope == "global" && ip == "93.184.216.34".parse::<IpAddr>().unwrap()).then(|| {
            ResolvedDomain {
                domain: "example.com.".into(),
                cnames: vec!["cdn.example.net.".into()],
                ..ResolvedDomain::default()
            }
        })
    }
}

struct Profiles;

impl ProfileLookup for Profiles {
    fn local_profile(&self, _process: &Process) -> Option<LocalProfileInfo> {
        Some(LocalProfileInfo {
            id: "profile-id".into(),
            name: "Firefox".into(),
            source: "local".into(),
            internal: false,
            enable_history: true,
            revision_cnt: 3,
        })
    }

    fn profile_source(&self, _process: &Process, option_key: &str) -> Option<String> {
        Some(format!("source-of-{option_key}"))
    }
}

fn profile_setup() -> Setup {
    let plain = setup();
    plain.resolver.add_process(4242, "firefox");
    plain.resolver.pids.lock().unwrap().insert(43210, 4242);
    let env = Environment::new(plain.resolver.clone(), common::new_entity_factory())
        .with_clock(plain.clock.clone())
        .with_profiles(Arc::new(Profiles))
        .with_domains(Arc::new(Domains));
    Setup {
        network: Network::new(env),
        clock: plain.clock,
        resolver: plain.resolver,
    }
}

#[test]
fn profile_and_domain_are_applied() {
    let setup = profile_setup();

    // An open DNS request for the domain is consumed by the connection.
    let dns = setup.network.new_connection_from_dns_request(
        "example.com.",
        vec![],
        "dns-1",
        ip("192.168.1.5"),
        43210,
    );
    setup.network.save_open_dns_request(
        Some(&OpenDnsQuery {
            qtype: DNS_TYPE_A,
            served_from_cache: false,
            requesting_new: false,
        }),
        &dns,
    );
    assert_eq!(setup.network.open_dns_request_count(), 1);

    let shared = setup
        .network
        .new_connection_from_first_packet(&outbound_tcp());
    {
        let mut conn = shared.lock().unwrap();
        assert_eq!(conn.scope, "example.com.");
        assert_eq!(conn.profile_revision_counter, 3);
        assert_eq!(conn.process_context.profile, "profile-id");
        assert_eq!(conn.process_context.profile_name, "Firefox");
        assert_eq!(conn.process_context.source, "local");
        assert!(conn.history_enabled);
        assert!(conn.bandwidth_enabled);
        let entity = conn.entity.as_ref().unwrap();
        assert_eq!(entity.domain, "example.com.");
        assert_eq!(entity.cname, vec!["cdn.example.net.".to_string()]);

        conn.set_verdict(Verdict::Block, "no", "filter/x", None);
        assert_eq!(conn.reason.option_key, "filter/x");
        assert_eq!(conn.reason.profile, "source-of-filter/x");
        conn.set_verdict(Verdict::Accept, "yes", "", None);
        assert_eq!(conn.reason.option_key, "");
        assert_eq!(conn.reason.profile, "");
    }
    assert_eq!(setup.network.open_dns_request_count(), 0);
}

#[test]
fn captive_portal_domain_is_used() {
    let plain = setup();
    let env = Environment::new(plain.resolver.clone(), common::new_entity_factory())
        .with_clock(plain.clock.clone())
        .with_captive_portal(Arc::new(|| {
            Some(pm_network::CaptivePortal {
                ip: "93.184.216.34".parse().unwrap(),
                domain: "portal.example.".into(),
            })
        }));
    let network = Network::new(env);
    let shared = network.new_connection_from_first_packet(&outbound_tcp());
    let conn = shared.lock().unwrap();
    assert_eq!(conn.scope, "portal.example.");
    assert_eq!(conn.entity.as_ref().unwrap().domain, "portal.example.");
}

#[test]
fn save_exposes_the_connection_and_pushes_updates() {
    let (setup, databases) = with_network();
    let interface = databases.internal_interface();
    let mut subscription = interface.subscribe(query("network:tree/")).unwrap();

    let shared = setup
        .network
        .new_connection_from_first_packet(&outbound_tcp());
    setup.network.save(&shared);

    let id = "6-192.168.1.5-43210-93.184.216.34-443";
    let key = format!("network:tree/4242/ip/{id}");
    {
        let conn = shared.lock().unwrap();
        assert_eq!(conn.key.as_ref().unwrap().to_string(), key);
        assert_eq!(conn.meta.created, START);
    }

    let pushed = subscription.try_recv().expect("update pushed");
    assert_eq!(pushed.key.to_string(), key);
    assert!(!pushed.data.is_empty());

    // Get by key, including the process record.
    let record = interface.get(&key).unwrap();
    let value: serde_json::Value = serde_json::from_slice(&record.data).unwrap();
    assert_eq!(value["ID"], id);
    assert_eq!(value["PID"], 4242);
    let process_key = setup.resolver.all_processes()[0].scoped_key();
    assert!(interface.get(&format!("network:{process_key}")).is_ok());
    // Unknown and malformed keys are not found.
    assert!(interface.get("network:tree/4242/ip/unknown").is_err());
    assert!(interface.get("network:tree/4242/xx/unknown").is_err());
    assert!(interface.get("network:tree/").is_err());
}

use pm_network::ProcessResolver;

#[test]
fn queries_filter_by_prefix_and_scope() {
    let (setup, databases) = with_network();
    let interface = databases.internal_interface();

    let ip_conn = setup
        .network
        .new_connection_from_first_packet(&outbound_tcp());
    setup.network.save(&ip_conn);
    let dns_conn = setup.network.new_connection_from_dns_request(
        "example.org.",
        vec![],
        "dns-1",
        ip("192.168.1.5"),
        43210,
    );
    setup.network.save(&dns_conn);

    let ip_key = "network:tree/4242/ip/6-192.168.1.5-43210-93.184.216.34-443".to_string();
    let dns_key = "network:tree/4242/dns/dns-1".to_string();
    let process_key = format!("network:{}", setup.resolver.all_processes()[0].scoped_key());

    let all = keys(&interface.query(&query("network:tree/")).unwrap());
    let mut expected = vec![ip_key.clone(), dns_key.clone(), process_key];
    expected.sort();
    assert_eq!(all, expected);

    assert_eq!(
        keys(&interface.query(&query("network:tree/4242/ip/")).unwrap()),
        vec![ip_key.clone()]
    );
    assert_eq!(
        keys(&interface.query(&query("network:tree/4242/dns/")).unwrap()),
        vec![dns_key]
    );

    // A where condition is applied to the JSON of connections.
    let filtered = Query::new("network:tree/4242/ip/")
        .where_(pm_query::Condition::new(
            "Entity.Port",
            pm_query::EQUALS,
            80i64,
        ))
        .check()
        .unwrap();
    assert!(interface.query(&filtered).unwrap().is_empty());
}

#[test]
fn dns_request_inherits_the_process_of_a_saved_dns_request_connection() {
    let (setup, _) = with_network();
    let info = packet(17, "192.168.1.5", 43210, "9.9.9.9", 53, false);
    let first = setup.network.new_connection_from_dns_request(
        "a.example.",
        vec![],
        "dns-a",
        ip("192.168.1.5"),
        43210,
    );
    assert_eq!(first.lock().unwrap().pid, 4242);
    setup.network.save_dns_request_connection(&first, &info);

    // The resolver would now answer differently; the saved connection wins.
    setup.resolver.add_process(1, "init");
    setup.resolver.pids.lock().unwrap().insert(43210, 1);
    let second = setup.network.new_connection_from_dns_request(
        "b.example.",
        vec![],
        "dns-b",
        ip("192.168.1.5"),
        43210,
    );
    {
        let second = second.lock().unwrap();
        assert_eq!(second.pid, 4242);
        assert_eq!(second.conn_type, ConnectionType::Dns);
        assert_eq!(second.scope, "b.example.");
        assert_eq!(second.started, second.ended);
        assert!(second.data_complete);
        assert_eq!(second.entity.as_ref().unwrap().domain, "b.example.");
    }

    // An ended connection is only used for 3 seconds.
    first.lock().unwrap().ended = START - 10;
    let third = setup.network.new_connection_from_dns_request(
        "c.example.",
        vec![],
        "dns-c",
        ip("192.168.1.5"),
        43210,
    );
    assert_eq!(third.lock().unwrap().pid, 1);
}

#[test]
fn dns_request_connections_are_not_saved_without_port_or_pid() {
    let (setup, _) = with_network();
    let conn = setup.network.new_connection_from_dns_request(
        "a.example.",
        vec![],
        "dns-a",
        ip("192.168.1.5"),
        43210,
    );
    let no_port = packet(17, "192.168.1.5", 0, "9.9.9.9", 53, false);
    setup.network.save_dns_request_connection(&conn, &no_port);
    assert!(setup.network.get_dns_request_connection(&no_port).is_none());

    conn.lock().unwrap().pid = -1;
    let info = packet(17, "192.168.1.5", 43210, "9.9.9.9", 53, false);
    setup.network.save_dns_request_connection(&conn, &info);
    assert!(setup.network.get_dns_request_connection(&info).is_none());
}

#[test]
fn external_dns_request_uses_the_network_host() {
    let (setup, _) = with_network();
    let conn = setup.network.new_connection_from_external_dns_request(
        "a.example.",
        vec![],
        "dns-ext",
        ip("192.168.1.77"),
    );
    let conn = conn.lock().unwrap();
    assert!(conn.external);
    assert_eq!(conn.pid, -255);
    assert_eq!(conn.process_context.process_name, "host-192.168.1.77");
}

#[test]
fn open_dns_requests_are_written_after_the_limit() {
    let (setup, databases) = with_network();
    let interface = databases.internal_interface();
    let q = |qtype, cached, new| {
        Some(OpenDnsQuery {
            qtype,
            served_from_cache: cached,
            requesting_new: new,
        })
    };

    let mk = |id: &str, domain: &str| {
        setup
            .network
            .new_connection_from_dns_request(domain, vec![], id, ip("192.168.1.5"), 43210)
    };

    // Missing answer or cached answer: not saved at all.
    setup
        .network
        .save_open_dns_request(None, &mk("dns-0", "none."));
    setup
        .network
        .save_open_dns_request(q(DNS_TYPE_A, true, false).as_ref(), &mk("dns-1", "cached."));
    assert_eq!(setup.network.open_dns_request_count(), 0);

    // Unsupported types are saved immediately.
    setup
        .network
        .save_open_dns_request(q(16, false, false).as_ref(), &mk("dns-2", "txt."));
    assert!(setup.network.get_dns_connection("dns-2").is_some());

    // Supported types wait; a later request for the same key replaces it.
    setup
        .network
        .save_open_dns_request(q(DNS_TYPE_A, false, false).as_ref(), &mk("dns-3", "a."));
    setup
        .network
        .save_open_dns_request(q(DNS_TYPE_A, true, true).as_ref(), &mk("dns-4", "a."));
    setup
        .network
        .save_open_dns_request(q(DNS_TYPE_SVCB, false, false).as_ref(), &mk("dns-5", "a."));
    assert_eq!(setup.network.open_dns_request_count(), 2);

    setup.network.write_open_dns_requests();
    assert_eq!(setup.network.open_dns_request_count(), 2, "too fresh");

    setup.clock.advance(4);
    setup.network.write_open_dns_requests();
    assert_eq!(setup.network.open_dns_request_count(), 0);
    assert!(setup.network.get_dns_connection("dns-3").is_none());
    assert!(setup.network.get_dns_connection("dns-4").is_some());
    assert!(setup.network.get_dns_connection("dns-5").is_some());
    assert!(interface.get("network:tree/4242/dns/dns-4").is_ok());
}

fn established(setup: &Setup, src_port: u16, protocol: u8) -> SharedConnection {
    setup.resolver.pids.lock().unwrap().insert(src_port, 4242);
    let info = packet(
        protocol,
        "192.168.1.5",
        src_port,
        "93.184.216.34",
        443,
        false,
    );
    let shared = setup.network.new_connection_from_first_packet(&info);
    setup.network.save(&shared);
    shared
}

#[test]
fn cleaner_ends_and_deletes_connections() {
    let (setup, databases) = with_network();
    let interface = databases.internal_interface();
    let mut subscription = interface.subscribe(query("network:tree/")).unwrap();

    let shared = established(&setup, 43210, 6);
    let id = shared.lock().unwrap().id.clone();
    while subscription.try_recv().is_some() {}
    setup.resolver.alive.lock().unwrap().insert(43210, true);

    // Fresh connections are skipped.
    setup.clock.advance(1);
    assert!(setup.network.clean_connections().is_empty());

    // Active connection stays and reports its PID.
    setup.clock.advance(30);
    let active = setup.network.clean_connections();
    assert!(active.contains(&4242));
    assert_eq!(shared.lock().unwrap().ended, 0);

    // The socket is gone: the connection is ended and saved.
    setup.resolver.alive.lock().unwrap().insert(43210, false);
    setup.clock.advance(30);
    let now = setup.clock.now();
    setup.network.clean_connections();
    assert_eq!(shared.lock().unwrap().ended, now);
    let update = subscription.try_recv().expect("ended update");
    let value: serde_json::Value = serde_json::from_slice(&update.data).unwrap();
    assert_eq!(value["Ended"], now);

    // Not deleted before the threshold, deleted after it.
    setup.clock.advance(599);
    setup.network.clean_connections();
    assert!(setup.network.get_connection(&id).is_some());
    setup.clock.advance(2);
    setup.network.clean_connections();
    assert!(setup.network.get_connection(&id).is_none());
    let deleted = subscription.try_recv().expect("delete update");
    assert!(deleted.meta.is_deleted());
    assert!(deleted.data.is_empty());
}

#[test]
fn cleaner_permanent_verdict_keeps_last_seen_fresh() {
    let (setup, _) = with_network();
    let shared = established(&setup, 43210, 6);
    shared.lock().unwrap().verdict_permanent = true;
    setup.resolver.alive.lock().unwrap().insert(43210, true);
    setup.clock.advance(100);
    let now = setup.clock.now();
    setup.network.clean_connections();
    assert_eq!(shared.lock().unwrap().last_seen, now);
}

#[test]
fn cleaner_ends_other_protocols_by_last_seen() {
    let (setup, _) = with_network();
    let icmp = established(&setup, 1, 1);
    let other = established(&setup, 2, 47);

    setup.clock.advance(30);
    setup.network.clean_connections();
    assert_eq!(icmp.lock().unwrap().ended, 0);
    assert_eq!(other.lock().unwrap().ended, 0);

    // ICMP ends after one minute, other protocols after five.
    setup.clock.advance(31);
    setup.network.clean_connections();
    assert_ne!(icmp.lock().unwrap().ended, 0);
    assert_eq!(other.lock().unwrap().ended, 0);

    setup.clock.advance(250);
    setup.network.clean_connections();
    assert_ne!(other.lock().unwrap().ended, 0);
}

#[test]
fn cleaner_removes_old_incomplete_connections_and_dns_requests() {
    let (setup, _) = with_network();
    let incomplete = setup.network.new_incomplete_connection(&outbound_tcp());
    let id = incomplete.lock().unwrap().id.clone();

    let dns = setup.network.new_connection_from_dns_request(
        "a.example.",
        vec![],
        "dns-a",
        ip("192.168.1.5"),
        43210,
    );
    setup.network.save(&dns);
    assert!(setup.network.get_dns_connection("dns-a").is_some());

    setup.clock.advance(59);
    setup.network.clean_connections();
    assert!(setup.network.get_connection(&id).is_some());

    setup.clock.advance(2);
    setup.network.clean_connections();
    assert!(setup.network.get_connection(&id).is_none());
    assert!(setup.network.get_dns_connection("dns-a").is_some());

    setup.clock.advance(540);
    setup.network.clean_connections();
    assert!(setup.network.get_dns_connection("dns-a").is_none());
}

#[test]
fn cleaner_drops_old_dns_request_connection_entries() {
    let (setup, _) = with_network();
    let info = packet(17, "192.168.1.5", 43210, "9.9.9.9", 53, false);
    let conn = setup.network.new_connection_from_dns_request(
        "a.example.",
        vec![],
        "dns-a",
        ip("192.168.1.5"),
        43210,
    );
    setup.network.save_dns_request_connection(&conn, &info);
    setup.clock.advance(10);
    setup.network.clean_connections();
    assert!(setup.network.get_dns_request_connection(&info).is_none());
}

#[test]
fn store_find_by_prefix_and_active_count() {
    let (setup, _) = with_network();
    let shared = established(&setup, 43210, 6);
    let store = &setup.network;
    assert_eq!(store.get_all_connections().len(), 1);
    assert!(store.get_dns_connection("x").is_none());
    shared.lock().unwrap().ended = 5;
    let id = shared.lock().unwrap().id.clone();
    assert_eq!(store.get_connection(&id).unwrap().lock().unwrap().ended, 5);
}

#[test]
fn db_key_parsing_matches_go() {
    let key = |p: &str, s: &str, i: &str| {
        Some(pm_network::ParsedKey {
            process_key: p.into(),
            scope: s.into(),
            id: i.into(),
        })
    };
    assert_eq!(parse_db_key(""), key("", "", ""));
    assert_eq!(parse_db_key("tree"), key("", "", ""));
    assert_eq!(parse_db_key("tree/"), key("", "", ""));
    assert_eq!(parse_db_key("tree/12"), key("12", "", ""));
    assert_eq!(parse_db_key("tree/12/"), key("12", "", ""));
    assert_eq!(parse_db_key("tree/12/ip"), key("12", "ip", ""));
    assert_eq!(parse_db_key("tree/12/dns/abc"), key("12", "dns", "abc"));
    assert_eq!(parse_db_key("tree/12/ip/"), key("12", "ip", ""));
    assert_eq!(parse_db_key("tree/12/foo/abc"), None);
    assert_eq!(parse_db_key("tree/12/ip/a/b"), None);
}

#[test]
fn storage_is_read_only() {
    let (_setup, databases) = with_network();
    let interface = databases.internal_interface();
    assert!(interface.delete("network:tree/1/ip/x").is_err());
}
