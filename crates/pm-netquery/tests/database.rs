//! Runs queries against a real database and compares the JSON with Go output.

use std::collections::BTreeMap;
use std::path::PathBuf;

use pm_netquery::{Conn, Database, QueryRequest};
use serde_json::Value;

fn fixture() -> Value {
    let raw = include_str!("../../fixtures/m0/netquery.json");
    serde_json::from_str::<Value>(raw).expect("fixture json")["rows"].clone()
}

struct TempHistory(PathBuf);

impl TempHistory {
    fn new(name: &str) -> TempHistory {
        let dir = std::env::temp_dir().join(format!("pm-netquery-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        TempHistory(dir)
    }
    fn file(&self) -> PathBuf {
        self.0.join("history.db")
    }
}

impl Drop for TempHistory {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

const STARTED: i64 = 1_704_164_645; // 2024-01-02 03:04:05 UTC

/// Same rows as `netqueryConns` in tools/rust-fixtures/netquery.go.
fn conns() -> Vec<Conn> {
    vec![
        Conn {
            id: "proc-1-conn-a".into(),
            profile_id: "local/abc".into(),
            path: "/usr/bin/curl".into(),
            conn_type: "ip".into(),
            external: true,
            ip_version: 4,
            ip_protocol: 6,
            local_ip: "10.0.0.2".into(),
            local_port: 40000,
            remote_ip: "93.184.216.34".into(),
            remote_port: 443,
            domain: "example.com.".into(),
            country: "US".into(),
            asn: 15133,
            as_owner: "Edgecast <Inc>".into(),
            latitude: 37.751,
            longitude: -97.822,
            scope: 4,
            worst_verdict: 2,
            active_verdict: 2,
            firewall_verdict: 2,
            started: STARTED,
            encrypted: true,
            direction: "outbound".into(),
            extra_data: br#"{"pid":12,"reason":{"Msg":"allowed"}}"#.to_vec(),
            allowed: Some(true),
            profile_revision: 3,
            active: true,
            profile_name: "curl".into(),
            ..Conn::default()
        },
        Conn {
            id: "proc-2-conn-b".into(),
            profile_id: "special/system".into(),
            conn_type: "dns".into(),
            domain: "blocked.test.".into(),
            ip_version: 6,
            ip_protocol: 17,
            local_ip: "::1".into(),
            remote_ip: "2001:db8::1".into(),
            remote_port: 53,
            worst_verdict: 3,
            active_verdict: 3,
            firewall_verdict: 3,
            started: STARTED + 1,
            ended: Some(STARTED + 90),
            tunneled: true,
            internal: true,
            direction: "inbound".into(),
            extra_data: b"{}".to_vec(),
            allowed: Some(false),
            exit_node: Some("exit-node-1".into()),
            profile_name: "System".into(),
            ..Conn::default()
        },
    ]
}

#[test]
fn encoded_params_match_go() {
    let f = fixture();
    for (conn, want) in conns().iter().zip(f["encoded"].as_array().unwrap()) {
        let want = want.as_object().unwrap();
        let got = conn.params();
        assert_eq!(got.len(), want.len());
        for (col, value) in got {
            let w = &want[col]["value"];
            let ok = match value {
                rusqlite::types::Value::Null => w.is_null(),
                rusqlite::types::Value::Integer(i) => {
                    w.as_i64() == Some(i) || w.as_bool().map(i64::from) == Some(i)
                }
                rusqlite::types::Value::Real(r) => w.as_f64() == Some(r),
                rusqlite::types::Value::Text(t) => w.as_str() == Some(t.as_str()),
                rusqlite::types::Value::Blob(b) => serde_json::from_slice::<Value>(&b).unwrap() == *w,
            };
            assert!(ok, "{}: column {col}: {w}", conn.id);
        }
    }
}

fn filled_db(tmp: &TempHistory) -> Database {
    let db = Database::open(&tmp.file()).unwrap();
    for c in conns() {
        db.save(&c, true).unwrap();
    }
    db.update_bandwidth(false, "proc-2", "conn-b", 99, 77, STARTED).unwrap();
    db
}

#[test]
fn query_results_match_go() {
    let tmp = TempHistory::new("results");
    let db = filled_db(&tmp);
    for case in fixture()["results"].as_array().unwrap() {
        let raw = case["request"].as_str().unwrap();
        let req = QueryRequest::from_json(&serde_json::from_str(raw).unwrap()).unwrap();

        let rows = db.query(&req).unwrap();
        let got = pm_dsd::gojson::to_vec_indent_unescaped(&rows, "", "  ").unwrap();
        assert_eq!(String::from_utf8(got).unwrap(), case["schema"].as_str().unwrap(), "{raw}");

        let batch = BTreeMap::from([("q".to_string(), req)]);
        let (results, errors) = db.query_batch(&batch).unwrap();
        assert!(errors.is_empty(), "{errors:?}");
        let got = pm_dsd::gojson::to_vec_indent_unescaped(&results["q"], "", "  ").unwrap();
        assert_eq!(String::from_utf8(got).unwrap(), case["batch"].as_str().unwrap(), "{raw}");
    }
}

#[test]
fn history_and_cleanup() {
    let tmp = TempHistory::new("cleanup");
    let db = filled_db(&tmp);
    // UNION (as in Go) merges proc-1, which is identical in main and history.
    assert_eq!(db.count_rows().unwrap(), 3);

    // Only the ended connection is older than the threshold.
    assert_eq!(db.cleanup(STARTED + 1000).unwrap(), 1);
    assert_eq!(db.count_rows().unwrap(), 2);

    // proc-1 in history now differs from the live row.
    db.mark_all_history_connections_ended(STARTED + 2000).unwrap();
    assert_eq!(db.count_rows().unwrap(), 3);
    let errors = db.cleanup_history(STARTED + 2 * 86_400, |p| if p == "local/abc" { 1 } else { 0 }).unwrap();
    assert!(errors.is_empty());
    assert_eq!(db.count_rows().unwrap(), 2);
    db.migrate_profile_id("special/system", "local/sys").unwrap();
    db.remove_history_for_profile("local/sys").unwrap();
    // Live row of proc-1 remains; history is empty now.
    assert_eq!(db.count_rows().unwrap(), 1);

    db.remove_all_history().unwrap();
    drop(db);
    // History persists in the file across reopen.
    let db = Database::open(&tmp.file()).unwrap();
    assert_eq!(db.count_rows().unwrap(), 0);
}

