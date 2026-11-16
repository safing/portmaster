//! Endpoint tests through `Api::handle` on a temporary history file.

use std::path::PathBuf;
use std::sync::Arc;

use pm_api::{Api, Request, Response, PERMIT_USER};
use pm_netquery::api::{register_api, ApiOptions};
use pm_netquery::{Conn, Database};
use serde_json::{json, Value};

struct Temp(PathBuf);

impl Temp {
    fn new(name: &str) -> Temp {
        let dir = std::env::temp_dir().join(format!("pm-netquery-api-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        Temp(dir)
    }
}

impl Drop for Temp {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

const STARTED: i64 = 1_704_164_645;

fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64
}

fn conn(id: &str, profile: &str, started: i64, domain: &str, verdict: i8) -> Conn {
    Conn {
        id: id.into(),
        profile_id: profile.into(),
        domain: domain.into(),
        conn_type: "dns".into(),
        started,
        active_verdict: verdict,
        extra_data: b"{}".to_vec(),
        ..Conn::default()
    }
}

struct Setup {
    api: Arc<Api>,
    db: Arc<Database>,
    _tmp: Temp,
}

fn setup(name: &str, dev: bool, retention: i64) -> Setup {
    let tmp = Temp::new(name);
    let db = Arc::new(Database::open(&tmp.0.join("history.db")).unwrap());
    let api = Api::new("127.0.0.1:0");
    api.update_api_keys(&["k?read=user&write=user".to_string()]);
    let mut opts = ApiOptions::new(Arc::new(move || dev), Arc::new(move |_| retention));
    opts.now_unix = Arc::new(|| STARTED + 10 * 86_400);
    register_api(&api, db.clone(), opts).unwrap();
    Setup { api, db, _tmp: tmp }
}

fn call(api: &Api, method: &str, path: &str, query: &str, body: &str) -> Response {
    let mut req = Request::new(method, format!("/api/v1/{path}"))
        .with_host("127.0.0.1:817")
        .with_header("authorization", "Bearer k")
        .with_body(body.as_bytes().to_vec());
    if !query.is_empty() {
        req = req.with_query(query);
    }
    api.handle(req)
}

fn json_body(r: &Response) -> Value {
    serde_json::from_slice(&r.body).unwrap_or_else(|_| panic!("not json: {}", r.body_string()))
}

#[test]
fn registration_matches_go() {
    let s = setup("reg", false, 0);
    let want = [
        ("netquery/query", true, true),
        ("netquery/query/batch", true, true),
        ("netquery/charts/connection-active", false, true),
        ("netquery/charts/bandwidth", false, true),
        ("netquery/history/clear", false, true),
        ("netquery/history/cleanup", false, true),
    ];
    for (path, read, write) in want {
        let ep = s.api.endpoint(path).unwrap_or_else(|| panic!("missing {path}"));
        assert_eq!(ep.read == PERMIT_USER, read, "{path} read");
        assert_eq!(ep.write == PERMIT_USER, write, "{path} write");
        assert_eq!(ep.read_method, if read { "GET" } else { "" }, "{path}");
        assert_eq!(ep.write_method, "POST", "{path}");
    }
    let q = s.api.endpoint("netquery/query").unwrap();
    assert_eq!(q.name, "Query Connections");
    assert_eq!(q.description, "Query the in-memory sqlite connection database.");
    assert_eq!(q.mime_type, "application/json");
    let b = s.api.endpoint("netquery/charts/bandwidth").unwrap();
    assert_eq!(b.name, "Bandwidth Chart");
    let c = s.api.endpoint("netquery/history/cleanup").unwrap();
    assert_eq!(c.name, "Apply connection history retention threshold");
}

#[test]
fn query_via_post_and_get() {
    let s = setup("query", false, 0);
    s.db.save(&conn("a", "local/x", STARTED, "a.test.", 2), false).unwrap();
    let body = r#"{"select":["id","domain"],"query":{"domain":"a.test."}}"#;
    let r = call(&s.api, "POST", "netquery/query", "", body);
    assert_eq!(r.status, 200);
    assert_eq!(
        r.body_string(),
        "{\n  \"results\": [\n    {\n      \"domain\": \"a.test.\",\n      \"id\": \"a\"\n    }\n  ]\n}\n"
    );
    let q = format!("q={}", body.replace('"', "%22").replace('{', "%7B").replace('}', "%7D"));
    let r2 = call(&s.api, "GET", "netquery/query", &q, "");
    assert_eq!(r2.status, 200);
    assert_eq!(r2.body, r.body);
}

#[test]
fn empty_result_is_null() {
    let s = setup("empty", false, 0);
    let r = call(&s.api, "POST", "netquery/query", "", r#"{"query":{"id":"none"}}"#);
    assert_eq!(r.status, 200);
    assert_eq!(r.body_string(), "{\n  \"results\": null\n}\n");
}

#[test]
fn bad_payloads_are_400() {
    let s = setup("bad", false, 0);
    let r = call(&s.api, "POST", "netquery/query", "", "{not json");
    assert_eq!(r.status, 400);
    assert!(r.body_string().starts_with("invalid query: "), "{}", r.body_string());
    // Go json.Unmarshal reports an empty body as a syntax error.
    let r = call(&s.api, "POST", "netquery/query", "", "");
    assert_eq!(r.status, 400);
    assert_eq!(r.body_string(), "invalid query: unexpected end of JSON input\n");
    let r = call(&s.api, "POST", "netquery/query", "", r#"{"query":{"nope":1}}"#);
    assert_eq!(r.status, 400);
    assert!(r.body_string().contains("generating where clause"), "{}", r.body_string());
}

#[test]
fn dev_mode_adds_echo_fields() {
    let s = setup("dev", true, 0);
    let r = call(&s.api, "POST", "netquery/query", "", r#"{"select":["id"],"query":{"id":"none"},"groupBy":["id"]}"#);
    assert_eq!(r.status, 200);
    let v = json_body(&r);
    assert_eq!(v["results"], Value::Null);
    assert!(v["sql_prep_stmt"].as_str().unwrap().starts_with("SELECT id FROM"));
    assert_eq!(v["query"]["id"][0]["$eq"], "none");
    assert_eq!(v["groupBy"], json!(["id"]));
    assert_eq!(v["orderBy"], Value::Null);
    assert_eq!(v["selects"][0]["field"], "id");
    assert!(v["sql_params"].is_object());
}

#[test]
fn batch_with_empty_entry() {
    let s = setup("batch", false, 0);
    s.db.save(&conn("a", "local/x", STARTED, "a.test.", 2), false).unwrap();
    let body = r#"{"full":{"select":["id"]},"empty":{"select":["id"],"query":{"id":"none"}}}"#;
    let r = call(&s.api, "POST", "netquery/query/batch", "", body);
    assert_eq!(r.status, 200);
    assert_eq!(
        r.body_string(),
        "{\n  \"empty\": null,\n  \"full\": [\n    {\n      \"id\": \"a\"\n    }\n  ]\n}\n"
    );
    let r = call(&s.api, "POST", "netquery/query/batch", "", r#"{"x":{"query":{"nope":1}}}"#);
    assert_eq!(r.status, 400);
    let r = call(&s.api, "POST", "netquery/query/batch", "", "{}");
    assert_eq!(r.status, 200);
    assert_eq!(r.body_string(), "{}\n");
}

#[test]
fn active_chart_counts_verdicts() {
    let s = setup("active", false, 0);
    let t = now() - 100;
    s.db.save(&conn("a", "local/x", t, "a.test.", 2), false).unwrap();
    s.db.save(&conn("b", "local/x", t, "b.test.", 3), false).unwrap();
    let r = call(&s.api, "POST", "netquery/charts/connection-active", "", r#"{"query":{"domain":"a.test."}}"#);
    assert_eq!(r.status, 200, "{}", r.body_string());
    let v = json_body(&r);
    assert!(v["query"].as_str().unwrap().contains("WHERE ( ( domain ="), "{}", v["query"]);
    assert!(v["params"].as_object().unwrap().values().any(|p| p == "a.test."));
    let rows = v["results"].as_array().unwrap();
    assert!(rows.len() >= 9);
    assert!(rows.iter().any(|r| r["value"].as_i64().unwrap_or(0) > 0 && r["countBlocked"] == 0));

    let r = call(&s.api, "POST", "netquery/charts/connection-active", "", "{}");
    let v = json_body(&r);
    assert_eq!(v["params"], json!({}));
    assert!(v["results"].as_array().unwrap().iter().any(|r| r["value"].as_i64().unwrap_or(0) > 0 && r["countBlocked"].as_i64().unwrap_or(0) > 0));

    let r = call(&s.api, "POST", "netquery/charts/connection-active", "", r#"{"query":{"nope":1}}"#);
    assert_eq!(r.status, 400);
    // The chart endpoints do not support GET.
    assert_eq!(call(&s.api, "GET", "netquery/charts/connection-active", "q=%7B%7D", "").status, 405);
}

#[test]
fn bandwidth_chart_sums_samples() {
    let s = setup("bw", false, 0);
    s.db.save(&conn("p-c", "local/x", STARTED, "a.test.", 2), false).unwrap();
    s.db.update_bandwidth(false, "p", "c", 10, 4, STARTED + 3).unwrap();
    s.db.update_bandwidth(false, "p", "c", 5, 1, STARTED + 4).unwrap();
    let r = call(&s.api, "POST", "netquery/charts/bandwidth", "", r#"{"interval":10,"groupBy":["profile"]}"#);
    assert_eq!(r.status, 200, "{}", r.body_string());
    let v = json_body(&r);
    assert!(v["query"].as_str().unwrap().contains("GROUP BY round(time/10, 0)*10, profile"));
    assert_eq!(v["params"], Value::Null);
    assert_eq!(v["results"][0]["incoming"], 15);
    assert_eq!(v["results"][0]["outgoing"], 5);
    assert_eq!(v["results"][0]["profile"], "local/x");
    assert_eq!(v["results"][0]["timestamp"], (STARTED + 3) / 10 * 10);

    let r = call(&s.api, "POST", "netquery/charts/bandwidth", "", r#"{"groupBy":["nope"]}"#);
    assert_eq!(r.status, 400);
    assert_eq!(r.body_string(), "unsupported groupBy key: \"nope\"\n");
    let r = call(&s.api, "POST", "netquery/charts/bandwidth", "", "{}");
    assert!(json_body(&r)["query"].as_str().unwrap().contains("round(time/10, 0)*10 as timestamp"));
}

#[test]
fn history_clear() {
    let s = setup("clear", false, 0);
    s.db.save(&conn("a", "local/x", STARTED, "a.test.", 2), true).unwrap();
    s.db.save(&conn("b", "local/y", STARTED, "b.test.", 2), true).unwrap();
    let hist = r#"{"databases":["history"],"select":["id"]}"#;
    let ids = |s: &Setup| json_body(&call(&s.api, "POST", "netquery/query", "", hist))["results"].clone();
    assert_eq!(ids(&s).as_array().unwrap().len(), 2);

    let r = call(&s.api, "POST", "netquery/history/clear", "", r#"{"profileIDs":["local/x"]}"#);
    assert_eq!(r.status, 200);
    assert_eq!(r.body_string(), "Successfully cleared history.\n");
    assert_eq!(ids(&s), json!([{"id": "b"}]));

    // No profile IDs clears everything.
    call(&s.api, "POST", "netquery/history/clear", "", "{}");
    assert_eq!(ids(&s), Value::Null);

    let r = call(&s.api, "POST", "netquery/history/clear", "", "nope");
    assert_eq!(r.status, 500);
    assert!(r.body_string().starts_with("failed to decode parameters in body: "));
}

#[test]
fn history_cleanup_uses_retention() {
    let s = setup("cleanup", false, 1);
    let mut old = conn("old", "local/x", STARTED, "a.test.", 2);
    old.ended = Some(STARTED + 5);
    s.db.save(&old, true).unwrap();
    let r = call(&s.api, "POST", "netquery/history/cleanup", "", "");
    assert_eq!(r.status, 200);
    assert_eq!(r.body_string(), "Deleted outdated connections.\n");
    let r = call(&s.api, "POST", "netquery/query", "", r#"{"databases":["history"],"select":["id"]}"#);
    assert_eq!(json_body(&r)["results"], Value::Null);
}

#[test]
fn content_type_and_batch_error_header() {
    let s = setup("ct", false, 0);
    let r = call(&s.api, "POST", "netquery/query", "", "{}");
    assert_eq!(r.headers.get("content-type").unwrap(), "text/plain; charset=utf-8");
    let r = call(&s.api, "POST", "netquery/charts/connection-active", "", "[]");
    assert_eq!(r.status, 400);
    assert!(r.body_string().contains("cannot unmarshal array"));
    let r = call(&s.api, "POST", "netquery/charts/connection-active", "", r#"{"query":{"nope":1}}"#);
    assert!(r.body_string().starts_with("1 error occurred:\n\t* "), "{}", r.body_string());
}

#[test]
fn batch_execution_error_sets_headers() {
    let s = setup("bat", false, 0);
    // `$sum` without field and condition generates `SUM()`: fine for the
    // generator, rejected by SQLite, so the batch fails at execution time.
    let r = call(
        &s.api,
        "POST",
        "netquery/query/batch",
        "",
        r#"{"ok":{"select":["id"]},"bad":{"select":[{"$sum":{"as":"x"}}]}}"#,
    );
    assert_eq!(r.status, 500);
    let errors: Vec<_> = r.headers.get_all("x-query-error").iter().collect();
    assert_eq!(errors.len(), 1);
    assert!(errors[0].to_str().unwrap().starts_with("bad: "), "{errors:?}");
    let body: serde_json::Value = serde_json::from_slice(&r.body).unwrap();
    assert_eq!(body["ok"], Value::Null);
}
