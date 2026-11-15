//! Compatibility with the JSON, keys and IDs produced by Go.

mod common;

use std::collections::BTreeMap;
use std::net::IpAddr;

use pm_geoip::Coordinates;
use pm_netstate::IpScope;
use pm_network::{
    create_connection_id, make_key, Connection, ConnectionType, DnsRequestContext, JsonIp,
    ProcessContext, Reason, SplitTunContext, Verdict,
};
use pm_resolver::{IpScope as ResolverScope, ResolverInfo};
use serde_json::{json, Value};

fn fixture() -> Value {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/network.json");
    serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap()
}

fn expected(name: &str) -> (String, String) {
    let fixture = fixture();
    let case = fixture["connections"]
        .as_array()
        .unwrap()
        .iter()
        .find(|case| case["name"] == name)
        .unwrap_or_else(|| panic!("no fixture {name}"));
    (
        case["key"].as_str().unwrap().to_string(),
        case["json"].as_str().unwrap().to_string(),
    )
}

fn ip(s: &str) -> Option<IpAddr> {
    Some(s.parse().unwrap())
}

fn entity() -> pm_intel::Entity {
    (common::new_entity_factory())()
}

fn check(name: &str, pid: i32, scope: &str, conn: &Connection) {
    let (key, json) = expected(name);
    let actual = String::from_utf8(pm_dsd::gojson::to_vec(conn).unwrap()).unwrap();
    assert_eq!(actual, json, "{name} json");
    assert_eq!(
        format!("network:{}", make_key(pid, scope, &conn.id)),
        key,
        "{name} key"
    );
}

#[test]
fn outbound_full_matches_go() {
    let mut e = entity();
    e.ip = ip("93.184.216.34");
    e.ip_scope = IpScope::Global;
    e.protocol = 6;
    e.port = 443;
    e.domain = "example.com".into();
    e.reverse_domain = "reverse.example.net.".into();
    e.cname = vec!["www.example.com.".into()];
    e.country = "US".into();
    e.coordinates = Some(Coordinates {
        accuracy_radius: 100,
        latitude: 37.751,
        longitude: -97.822,
    });
    e.asn = 15133;
    e.as_org = "Edgecast <Inc> & Co".into();
    e.blocked_by_lists = vec!["TRAC".into()];
    e.blocked_entities = vec!["example.com".into()];
    e.list_occurences = BTreeMap::from([
        (
            "example.com".to_string(),
            vec!["TRAC".to_string(), "MAL".to_string()],
        ),
        ("AS15133".to_string(), vec!["MAL".to_string()]),
    ]);

    let conn = Connection {
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
    };
    check("ip_outbound_full", 4242, "ip", &conn);
}

#[test]
fn inbound_minimal_matches_go() {
    let mut e = entity();
    e.ip = ip("2001:db8::2");
    e.ip_scope = IpScope::Global;
    e.protocol = 17;
    e.port = 40000;

    let conn = Connection {
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
    };
    check("ip_inbound_minimal", -3, "ip", &conn);
}

#[test]
fn icmp_with_mapped_address_matches_go() {
    let mut e = entity();
    e.ip = ip("8.8.8.8");
    e.ip_scope = IpScope::Global;
    e.protocol = 1;

    let conn = Connection {
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
    };
    check("ip_icmp_mapped", -2, "ip", &conn);
}

#[test]
fn dns_request_matches_go() {
    let mut e = entity();
    e.ip_scope = IpScope::Global;
    e.domain = "example.org.".into();

    let mut conn = Connection {
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
    conn.set_verdict(Verdict::Block, "blocked by list", "", None);
    check("dns_request", 4242, "dns", &conn);
}

#[test]
fn connection_ids_match_go() {
    let fixture = fixture();
    for case in fixture["ids"].as_array().unwrap() {
        let parse = |field: &str| case[field].as_str().unwrap().parse::<IpAddr>().unwrap();
        let id = create_connection_id(
            case["protocol"].as_u64().unwrap() as u8,
            Some(parse("src")),
            case["src_port"].as_u64().unwrap() as u16,
            Some(parse("dst")),
            case["dst_port"].as_u64().unwrap() as u16,
            case["inbound"].as_bool().unwrap(),
        );
        assert_eq!(id, case["id"].as_str().unwrap(), "{case}");
    }
}

#[test]
fn verdict_names_match_go() {
    let fixture = fixture();
    for case in fixture["verdicts"].as_array().unwrap() {
        let value = case["value"].as_i64().unwrap() as i8;
        match Verdict::from_i8(value) {
            Some(verdict) => {
                assert_eq!(verdict as i8, value);
                assert_eq!(verdict.to_string(), case["string"].as_str().unwrap());
                assert_eq!(verdict.verb(), case["verb"].as_str().unwrap());
            }
            None => assert_eq!(case["string"], "<INVALID VERDICT>"),
        }
    }
}

#[test]
fn verdict_order_follows_values() {
    assert!(Verdict::Accept < Verdict::Block);
    assert!(Verdict::Block < Verdict::Drop);
    assert!(Verdict::Failed < Verdict::RerouteToSplitTun);
}
