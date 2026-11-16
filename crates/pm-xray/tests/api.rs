//! Endpoint registration and round-trip tests.

use std::sync::Arc;

use pm_api::{Api, Request, PERMIT_ADMIN, PERMIT_USER};
use pm_config::Config;
use pm_database::{Databases, MemoryStorage};
use pm_geoip::GeoIp;
use pm_xray::{AddSubscriptionRequest, ConnectRequest, ProcessAdapter, Xray, XRAY_DB};

struct NoopAdapter;
impl ProcessAdapter for NoopAdapter {
    fn spawn(
        &self,
        _binary: &str,
        _config: &std::path::Path,
    ) -> Result<Box<dyn pm_xray::ChildHandle>, String> {
        Err("not spawned in API test".into())
    }
}

fn env() -> (Arc<Api>, Arc<Xray>) {
    let config = Arc::new(Config::new());
    let databases = Arc::new(Databases::without_disk());
    databases.register_injected(XRAY_DB, "test").unwrap();
    databases
        .inject(XRAY_DB, Box::new(MemoryStorage::new()))
        .unwrap();
    let dir = std::env::temp_dir().join(format!("pm-xray-api-test-{}", std::process::id()));
    let xray = Xray::with_adapter(
        &config,
        &databases,
        Arc::new(GeoIp::new()),
        dir,
        "/opt/bin/xray-rust".into(),
        Box::new(NoopAdapter),
    );
    let api = Api::new("127.0.0.1:0");
    api.set_dev_mode(true);
    xray.register_api(&api).unwrap();
    (api, xray)
}

fn request(method: &str, path: &str, query: &str, body: Vec<u8>) -> Request {
    let mut request = Request::new(method, path);
    request.query_string = query.to_string();
    request.query = pm_api::parse_query(query);
    let has_body = !body.is_empty();
    request.body = body;
    if has_body {
        request = request.with_header("content-type", "application/json");
    }
    request
}

#[test]
fn endpoints_have_expected_paths_and_permissions() {
    let (api, _) = env();
    let expected = [
        ("xray/subscriptions", PERMIT_USER, PERMIT_ADMIN),
        ("xray/subscriptions/{id}", pm_api::NOT_SUPPORTED, PERMIT_ADMIN),
        ("xray/nodes", PERMIT_USER, pm_api::NOT_SUPPORTED),
        ("xray/connect", pm_api::NOT_SUPPORTED, PERMIT_ADMIN),
        ("xray/disconnect", pm_api::NOT_SUPPORTED, PERMIT_ADMIN),
        ("xray/status", PERMIT_USER, pm_api::NOT_SUPPORTED),
    ];
    for (path, read, write) in expected {
        let endpoint = api.endpoint(path).unwrap_or_else(|| panic!("missing {path}"));
        assert_eq!(endpoint.read, read, "{path} read");
        assert_eq!(endpoint.write, write, "{path} write");
    }
}

#[test]
fn ui_payload_compatibility() {
    let connect: ConnectRequest =
        serde_json::from_str(r#"{"id":"node-1","nodeId":"node-1"}"#).unwrap();
    assert_eq!(connect.node_id, "node-1");
    let add: AddSubscriptionRequest =
        serde_json::from_str(r#"{"url":"https://example.com/sub"}"#).unwrap();
    assert!(add.name.is_empty());
}

#[test]
fn subscription_crud_and_status_over_api() {
    let (api, xray) = env();

    let response = api.handle(request("GET", "/api/v1/xray/status", "", Vec::new()));
    assert_eq!(response.status, 200, "{}", response.body_string());
    let status: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(status["state"], "stopped");
    assert_eq!(status["binary"], "/opt/bin/xray-rust");

    // Disabled subscription: no network access during the test.
    let response = api.handle(request(
        "POST",
        "/api/v1/xray/subscriptions",
        "",
        br#"{"name":"Test","url":"https://example.com/sub","enabled":false}"#.to_vec(),
    ));
    assert_eq!(response.status, 200, "{}", response.body_string());
    let created: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    let id = created["id"].as_str().unwrap();

    let response = api.handle(request("GET", "/api/v1/xray/subscriptions", "", Vec::new()));
    assert_eq!(response.status, 200, "{}", response.body_string());
    let list: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(list.as_array().unwrap().len(), 1);

    let response = api.handle(request("GET", "/api/v1/xray/nodes", "", Vec::new()));
    assert_eq!(response.status, 200, "{}", response.body_string());
    let nodes: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(nodes.as_array().unwrap().len(), 0);

    let query = format!("id={id}");
    let response = api.handle(request(
        "DELETE",
        "/api/v1/xray/subscriptions",
        &query,
        Vec::new(),
    ));
    assert_eq!(response.status, 200, "{}", response.body_string());
    assert!(xray.store.list().unwrap().is_empty());

    let response = api.handle(request(
        "POST",
        "/api/v1/xray/connect",
        "",
        br#"{"node_id":"missing"}"#.to_vec(),
    ));
    assert_eq!(response.status, 404, "{}", response.body_string());
}
