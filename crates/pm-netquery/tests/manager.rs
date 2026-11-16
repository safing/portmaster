//! Manager tests: conversion against Go fixtures, saving, runtime pushes and
//! the cleaner workers.

use std::collections::BTreeMap;
use std::net::IpAddr;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_database::{Databases, Interface, MemoryStorage};
use pm_geoip::Coordinates;
use pm_intel::{cache_interface, Entity, FilterLists, StaticLocations};
use pm_netquery::workers::{
    cleanup_history_once, cleanup_live_once, run_history_cleaner_every, run_live_cleaner_every,
    shutdown, HistoryRetention,
};
use pm_netquery::{convert_connection, ConvertError, Database, Manager, QueryRequest, PUSH_PREFIX};
use pm_netstate::IpScope;
use pm_network::{
    Connection, ConnectionType, DnsRequestContext, JsonIp, ProcessContext, Reason, SharedConnection,
    SplitTunContext, Verdict,
};
use pm_process::Process;
use pm_query::Query;
use pm_resolver::{IpScope as ResolverScope, ResolverInfo};
use pm_runtime::Registry;
use serde_json::{json, Value};
use tokio_util::sync::CancellationToken;

struct Temp(PathBuf);

impl Temp {
    fn new(name: &str) -> Temp {
        let dir = std::env::temp_dir().join(format!("pm-netquery-mgr-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        Temp(dir)
    }

    fn db(&self) -> Arc<Database> {
        Arc::new(Database::open(&self.0.join("history.db")).unwrap())
    }
}

impl Drop for Temp {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

fn fixture() -> Value {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/netquery.json");
    serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap()
}

fn ip(s: &str) -> Option<IpAddr> {
    Some(s.parse().unwrap())
}

fn entity() -> Entity {
    let databases = Arc::new(Databases::without_disk());
    databases.register_injected("cache", "Cache").unwrap();
    databases.inject("cache", Box::new(MemoryStorage::new())).unwrap();
    let lists = Arc::new(FilterLists::with_capacity(cache_interface(&databases), 100, 10, 5, 20, 20));
    Entity::new(lists, Arc::new(StaticLocations::new()))
}

fn with_process(mut conn: Connection, pid: i32) -> Connection {
    conn.process = Some(Arc::new(Process::empty(&format!("{pid}-1700000000"))));
    conn
}

/// The connections of the Go network fixture, in fixture order, plus two
/// extra cases.
fn connections() -> Vec<(&'static str, Connection)> {
    let mut out = Vec::new();

    let mut e = entity();
    e.ip = ip("93.184.216.34");
    e.ip_scope = IpScope::Global;
    e.protocol = 6;
    e.port = 443;
    e.domain = "example.com".into();
    e.reverse_domain = "reverse.example.net.".into();
    e.cname = vec!["www.example.com.".into()];
    e.country = "US".into();
    e.coordinates = Some(Coordinates { accuracy_radius: 100, latitude: 37.751, longitude: -97.822 });
    e.asn = 15133;
    e.as_org = "Edgecast <Inc> & Co".into();
    e.blocked_by_lists = vec!["TRAC".into()];
    e.blocked_entities = vec!["example.com".into()];
    e.list_occurences = BTreeMap::from([
        ("example.com".to_string(), vec!["TRAC".to_string(), "MAL".to_string()]),
        ("AS15133".to_string(), vec!["MAL".to_string()]),
    ]);
    out.push((
        "ip_outbound_full",
        with_process(
            Connection {
                id: "6-192.168.1.5-43210-93.184.216.34-443".into(),
                conn_type: ConnectionType::Ip,
                scope: "example.com".into(),
                ip_version: 4,
                ip_protocol: 6,
                local_ip: ip("192.168.1.5"),
                local_ip_scope: ResolverScope::SiteLocal,
                local_port: 43210,
                pid: 4242,
                entity: Some(e),
                resolver: Some(ResolverInfo {
                    name: "Cloudflare".into(),
                    r#type: "dot".into(),
                    source: "config".into(),
                    ip: ip("1.1.1.1"),
                    domain: "cloudflare-dns.com".into(),
                    ip_scope: ResolverScope::Global,
                    port: 853,
                }),
                verdict: Verdict::RerouteToSplitTun,
                connection_established: true,
                reason: Reason {
                    msg: "allowed by rule".into(),
                    option_key: "filter/endpoints".into(),
                    profile: "local/abc".into(),
                    context: Some(json!({"a": "x", "b": 2, "list": ["p", "q"]})),
                },
                started: 1_700_000_000,
                ended: 1_700_000_100,
                verdict_permanent: true,
                inspecting: true,
                tunneled: true,
                encrypted: true,
                tunnel_opts: Some(json!({"mode": "fast"})),
                process_context: ProcessContext {
                    process_name: "firefox".into(),
                    profile_name: "Firefox".into(),
                    binary_path: "/usr/lib/firefox/firefox".into(),
                    cmd_line: "firefox --new-window".into(),
                    pid: 4242,
                    created_at: 1_700_000_000_000,
                    profile: "abc".into(),
                    source: "local".into(),
                },
                dns_context: Some(DnsRequestContext {
                    domain: "example.com.".into(),
                    question: "example.com. IN A".into(),
                    rcode: "NOERROR".into(),
                    served_from_cache: true,
                    requesting_new: false,
                    is_backup: true,
                    filtered: false,
                    modified: "2023-11-14T22:13:20Z".into(),
                    expires: "2023-11-14T22:18:20.123456Z".into(),
                }),
                split_tun_context: Some(SplitTunContext {
                    interface: "wg0".into(),
                    ip: JsonIp(ip("10.8.0.2")),
                    proxy: "socks5://proxy.example:1080".into(),
                }),
                history_enabled: true,
                bandwidth_enabled: true,
                bytes_received: 1 << 40,
                bytes_sent: 12345,
                internal: true,
                profile_revision_counter: 7,
                ..Connection::default()
            },
            4242,
        ),
    ));

    let mut e = entity();
    e.ip = ip("2001:db8::2");
    e.ip_scope = IpScope::Global;
    e.protocol = 17;
    e.port = 40000;
    out.push((
        "ip_inbound_minimal",
        with_process(
            Connection {
                id: "17-2001:db8::1-5353-2001:db8::2-40000".into(),
                conn_type: ConnectionType::Ip,
                scope: "II".into(),
                ip_version: 6,
                inbound: true,
                ip_protocol: 17,
                local_ip: ip("2001:db8::1"),
                local_ip_scope: ResolverScope::Global,
                local_port: 5353,
                pid: -3,
                entity: Some(e),
                verdict: Verdict::Drop,
                started: 1_700_000_200,
                process_context: ProcessContext {
                    process_name: "Network Noise".into(),
                    pid: -3,
                    ..ProcessContext::default()
                },
                split_tun_context: Some(SplitTunContext {
                    interface: "eth0".into(),
                    ip: JsonIp(ip("192.0.2.9")),
                    proxy: String::new(),
                }),
                ..Connection::default()
            },
            -3,
        ),
    ));

    let mut e = entity();
    e.ip = ip("8.8.8.8");
    e.ip_scope = IpScope::Global;
    e.protocol = 1;
    out.push((
        "ip_icmp_mapped",
        with_process(
            Connection {
                id: "1-10.0.0.2-8.8.8.8".into(),
                conn_type: ConnectionType::Ip,
                scope: "PI".into(),
                ip_version: 4,
                ip_protocol: 1,
                local_ip: ip("::ffff:10.0.0.2"),
                local_ip_scope: ResolverScope::SiteLocal,
                pid: -2,
                entity: Some(e),
                started: 1_700_000_300,
                ..Connection::default()
            },
            -2,
        ),
    ));

    let mut e = entity();
    e.ip_scope = IpScope::Global;
    e.domain = "example.org.".into();
    let mut dns = Connection {
        id: "dns-4242-1".into(),
        conn_type: ConnectionType::Dns,
        external: true,
        scope: "example.org.".into(),
        pid: 4242,
        entity: Some(e),
        started: 1_700_000_400,
        ended: 1_700_000_400,
        process_context: ProcessContext {
            process_name: "curl".into(),
            pid: 4242,
            ..ProcessContext::default()
        },
        ..Connection::default()
    };
    dns.set_verdict(Verdict::Block, "blocked by list", "", None);
    out.push(("dns_request", with_process(dns, 4242)));

    let mut e = entity();
    e.ip = ip("1.1.1.1");
    e.ip_scope = IpScope::Global;
    e.protocol = 6;
    e.port = 443;
    e.domain = "one.example".into();
    assert!(!e.match_lists(&["NOPE".to_string()]));
    out.push((
        "ip_matched_empty_lists",
        with_process(
            Connection {
                id: "6-192.168.1.5-50000-1.1.1.1-443".into(),
                conn_type: ConnectionType::Ip,
                scope: "one.example".into(),
                ip_version: 4,
                ip_protocol: 6,
                local_ip: ip("192.168.1.5"),
                local_port: 50000,
                pid: 4242,
                entity: Some(e),
                started: 1_700_000_500,
                ..Connection::default()
            },
            4242,
        ),
    ));

    out.push((
        "no_entity_inbound",
        with_process(
            Connection {
                id: "no-entity".into(),
                inbound: true,
                started: 1_700_000_600,
                process_context: ProcessContext {
                    pid: 7,
                    source: "special".into(),
                    profile: "system".into(),
                    profile_name: "System".into(),
                    ..ProcessContext::default()
                },
                ..Connection::default()
            },
            7,
        ),
    ));

    out.push((
        "empty_entity_ended",
        with_process(
            Connection {
                id: "empty-entity".into(),
                conn_type: ConnectionType::Ip,
                entity: Some(entity()),
                started: 1_700_000_700,
                ended: 1_700_000_800,
                ..Connection::default()
            },
            8,
        ),
    ));
    out
}

fn conn_named(name: &str) -> Connection {
    connections().into_iter().find(|(n, _)| *n == name).unwrap().1
}

#[test]
fn convert_connection_matches_go() {
    let fx = fixture();
    let cases = fx["converted"].as_array().unwrap();
    let conns = connections();
    assert_eq!(cases.len(), conns.len());
    for (case, (name, conn)) in cases.iter().zip(&conns) {
        assert_eq!(case["name"], *name);
        let row = convert_connection(conn).unwrap();
        assert_eq!(
            String::from_utf8(row.extra_data.clone()).unwrap(),
            case["extra_data"].as_str().unwrap(),
            "{name} extra_data"
        );
        let json = String::from_utf8(pm_dsd::gojson::to_vec(&row).unwrap()).unwrap();
        assert_eq!(json, case["json"].as_str().unwrap(), "{name} json");
    }
}

#[test]
fn convert_connection_needs_a_process() {
    let conn = Connection::default();
    assert!(matches!(convert_connection(&conn), Err(ConvertError::NoProcess)));
}

fn shared(mut conn: Connection, complete: bool) -> SharedConnection {
    conn.data_complete = complete;
    Arc::new(Mutex::new(conn))
}

fn ids(db: &Database, database: &str) -> Vec<String> {
    let req = QueryRequest::from_json(&json!({"select": ["id"], "databases": [database], "orderBy": ["id"]})).unwrap();
    db.query(&req).unwrap().iter().map(|r| r["id"].as_str().unwrap().to_string()).collect()
}

fn setup(name: &str) -> (Temp, Arc<Database>, Manager, Interface) {
    let tmp = Temp::new(name);
    let db = tmp.db();
    let databases = Arc::new(Databases::without_disk());
    databases.register_injected("runtime", "Runtime").unwrap();
    let registry = Registry::new();
    let manager = Manager::new(db.clone(), PUSH_PREFIX, &registry).unwrap();
    registry.inject_as_database(&databases, "runtime").unwrap();
    (tmp, db, manager, databases.internal_interface())
}

#[test]
fn handle_saves_and_history_only_when_enabled() {
    let (_tmp, db, manager, _iface) = setup("handle");
    let mut with_history = conn_named("ip_outbound_full");
    with_history.history_enabled = true;
    let mut without_history = conn_named("ip_inbound_minimal");
    without_history.history_enabled = false;

    manager.handle(&shared(with_history, true));
    manager.handle(&shared(without_history, true));

    assert_eq!(
        ids(&db, "main"),
        vec!["-3-1700000000-17-2001:db8::1-5353-2001:db8::2-40000", "4242-1700000000-6-192.168.1.5-43210-93.184.216.34-443"]
    );
    assert_eq!(ids(&db, "history"), vec!["4242-1700000000-6-192.168.1.5-43210-93.184.216.34-443"]);
}

#[test]
fn handle_skips_incomplete_and_unconvertible_connections() {
    let (_tmp, db, manager, iface) = setup("skip");
    let mut sub = iface.subscribe(Query::new("runtime:")).unwrap();
    manager.handle(&shared(conn_named("ip_outbound_full"), false));
    manager.handle(&shared(Connection::default(), true));
    assert!(ids(&db, "main").is_empty());
    assert!(sub.try_recv().is_none());
}

#[test]
fn handle_pushes_the_runtime_record() {
    let (_tmp, _db, manager, iface) = setup("push");
    let mut sub = iface.subscribe(Query::new("runtime:")).unwrap();
    let mut conn = conn_named("dns_request");
    conn.meta.created = 11;
    conn.meta.modified = 12;
    manager.handle(&shared(conn, true));

    let update = sub.try_recv().expect("runtime update");
    assert_eq!(update.key.db, "runtime");
    assert_eq!(update.key.key, "netquery/data/4242-1700000000-dns-4242-1");
    assert_eq!(update.format, pm_dsd::format::JSON);
    assert_eq!((update.meta.created, update.meta.modified), (11, 12));
    let expected = fixture()["converted"][3]["json"].as_str().unwrap().to_string();
    assert_eq!(String::from_utf8(update.data.clone()).unwrap(), expected);
}

#[tokio::test]
async fn handle_feed_stops_when_closed_or_cancelled() {
    let (_tmp, db, manager, _iface) = setup("feed");
    let manager = Arc::new(manager);
    let (tx, rx) = tokio::sync::mpsc::channel(4);
    let task = tokio::spawn({
        let manager = manager.clone();
        async move { manager.handle_feed(rx, CancellationToken::new()).await }
    });
    tx.send(shared(conn_named("dns_request"), true)).await.unwrap();
    drop(tx);
    tokio::time::timeout(Duration::from_secs(5), task).await.unwrap().unwrap();
    assert_eq!(ids(&db, "main").len(), 1);

    let (tx, rx) = tokio::sync::mpsc::channel(1);
    let cancel = CancellationToken::new();
    let task = tokio::spawn({
        let cancel = cancel.clone();
        async move { manager.handle_feed(rx, cancel).await }
    });
    cancel.cancel();
    tokio::time::timeout(Duration::from_secs(5), task).await.unwrap().unwrap();
    drop(tx);
}

struct Retention;

impl HistoryRetention for Retention {
    fn local_profile_keep_days(&self, id: &str) -> Option<i64> {
        (id == "keepforever").then_some(0)
    }
    fn global_keep_days(&self) -> i64 {
        1
    }
}

fn row(id: &str, profile: &str, started: i64, ended: Option<i64>) -> pm_netquery::Conn {
    pm_netquery::Conn {
        id: id.into(),
        profile_id: profile.into(),
        started,
        ended,
        active: ended.is_none(),
        extra_data: b"{}".to_vec(),
        ..pm_netquery::Conn::default()
    }
}

#[test]
fn cleanup_passes_use_go_thresholds() {
    let tmp = Temp::new("cleanup");
    let db = tmp.db();
    let now = 1_700_000_000;
    // Live: ended 11 minutes ago is removed, 9 minutes ago stays.
    db.save(&row("old", "local/a", now - 3000, Some(now - 660)), false).unwrap();
    db.save(&row("recent", "local/a", now - 3000, Some(now - 540)), false).unwrap();
    db.save(&row("active", "local/a", now - 3000, None), false).unwrap();
    cleanup_live_once(&db, now);
    assert_eq!(ids(&db, "main"), vec!["active", "recent"]);

    // History: the global setting is 1 day, "keepforever" keeps everything.
    let old = now - 3 * 86_400;
    db.save(&row("h-old", "local/x", old, Some(old + 1)), true).unwrap();
    db.save(&row("h-new", "local/x", now - 60, Some(now - 30)), true).unwrap();
    db.save(&row("h-active", "local/x", old, None), true).unwrap();
    db.save(&row("h-forever", "local/keepforever", old, Some(old + 1)), true).unwrap();
    cleanup_history_once(&db, now, &Retention);
    assert_eq!(ids(&db, "history"), vec!["h-active", "h-forever", "h-new"]);
}

#[test]
fn shutdown_marks_history_ended() {
    let tmp = Temp::new("shutdown");
    let db = tmp.db();
    db.save(&row("a", "local/a", 1_700_000_000, None), true).unwrap();
    shutdown(&db);
    let req = QueryRequest::from_json(&json!({"select": ["active"], "databases": ["history"]})).unwrap();
    let rows = db.query(&req).unwrap();
    assert_eq!(rows[0]["active"], json!(false));
}

#[tokio::test]
async fn cleaner_loops_run_and_stop() {
    let tmp = Temp::new("loops");
    let db = tmp.db();
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64;
    db.save(&row("old", "local/a", now - 7200, Some(now - 3600)), false).unwrap();
    let old = now - 3 * 86_400;
    db.save(&row("h-old", "local/x", old, Some(old + 1)), true).unwrap();

    let cancel = CancellationToken::new();
    let live = tokio::spawn(run_live_cleaner_every(db.clone(), Duration::from_millis(10), cancel.clone()));
    let hist = tokio::spawn(run_history_cleaner_every(
        db.clone(),
        Arc::new(Retention),
        Duration::from_millis(10),
        Duration::from_millis(10),
        cancel.clone(),
    ));
    let deadline = std::time::Instant::now() + Duration::from_secs(5);
    while std::time::Instant::now() < deadline {
        if ids(&db, "main").is_empty() && ids(&db, "history").is_empty() {
            break;
        }
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
    assert!(ids(&db, "main").is_empty());
    assert!(ids(&db, "history").is_empty());
    cancel.cancel();
    tokio::time::timeout(Duration::from_secs(5), async {
        live.await.unwrap();
        hist.await.unwrap();
    })
    .await
    .unwrap();
}

#[test]
fn reason_context_keys_are_sorted_like_go() {
    let mut conn = conn_named("ip_outbound_full");
    conn.reason.context = Some(json!({"z": 1, "a": {"y": 1, "b": [{"d": 1, "c": 2}]}}));
    let row = convert_connection(&conn).unwrap();
    let extra = String::from_utf8(row.extra_data).unwrap();
    assert!(extra.contains(r#""Context":{"a":{"b":[{"c":2,"d":1}],"y":1},"z":1}"#), "{extra}");
}

#[test]
fn shutdown_vacuums_and_keeps_the_history_usable() {
    let tmp = Temp::new("vacuum");
    let db = tmp.db();
    for i in 0..200 {
        db.save(&row(&format!("c{i}"), "local/a", 1_700_000_000, Some(1_700_000_001)), true).unwrap();
    }
    db.remove_all_history().unwrap();
    shutdown(&db);
    assert!(ids(&db, "history").is_empty());
    db.save(&row("after", "local/a", 1_700_000_000, Some(1_700_000_001)), true).unwrap();
    assert_eq!(ids(&db, "history"), vec!["after"]);
}
