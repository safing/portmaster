//! Endpoint registration and round-trip tests.

use std::sync::Arc;

use pm_api::{Api, Request, PERMIT_ADMIN, PERMIT_USER};
use pm_config::Config;
use pm_database::{Databases, MemoryStorage};
use pm_geoip::GeoIp;
use pm_xray::{
    AddSubscriptionRequest, ConnectRequest, InboundProtocol, ProcessAdapter, StatsError,
    StatsService, TrafficCounter, Xray, XrayNode, XRAY_DB,
};

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

struct FakeChild;
impl pm_xray::ChildHandle for FakeChild {
    fn id(&self) -> u32 {
        42
    }
    fn try_wait(&mut self) -> Result<Option<i32>, String> {
        Ok(None)
    }
    fn kill(&mut self) -> Result<(), String> {
        Ok(())
    }
    fn wait(&mut self) -> Result<i32, String> {
        Ok(0)
    }
}

struct RunningAdapter;
impl ProcessAdapter for RunningAdapter {
    fn spawn(
        &self,
        _binary: &str,
        _config: &std::path::Path,
    ) -> Result<Box<dyn pm_xray::ChildHandle>, String> {
        Ok(Box::new(FakeChild))
    }
}

fn env() -> (Arc<Api>, Arc<Xray>) {
    env_with_stats(Arc::new(pm_xray::NoopTrafficStats))
}

fn env_with_stats(stats: Arc<dyn StatsService>) -> (Arc<Api>, Arc<Xray>) {
    env_with_stats_and_adapter(stats, Box::new(NoopAdapter))
}

fn env_with_stats_and_adapter(
    stats: Arc<dyn StatsService>,
    adapter: Box<dyn ProcessAdapter>,
) -> (Arc<Api>, Arc<Xray>) {
    let config = Arc::new(Config::new());
    let databases = Arc::new(Databases::without_disk());
    databases.register_injected(XRAY_DB, "test").unwrap();
    databases
        .inject(XRAY_DB, Box::new(MemoryStorage::new()))
        .unwrap();
    let dir = std::env::temp_dir().join(format!("pm-xray-api-test-{}", std::process::id()));
    let xray = Xray::with_adapter_and_stats(
        &config,
        &databases,
        Arc::new(GeoIp::new()),
        dir,
        "/opt/bin/xray-rust".into(),
        adapter,
        stats,
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
        ("xray/stats", PERMIT_USER, pm_api::NOT_SUPPORTED),
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

#[derive(Default)]
struct FakeStats;

impl StatsService for FakeStats {
    fn get_stats(&self, name: &str) -> Result<TrafficCounter, StatsError> {
        Ok(TrafficCounter {
            name: name.to_owned(),
            value: 11,
        })
    }

    fn query_stats(&self, _pattern: &str) -> Result<Vec<TrafficCounter>, StatsError> {
        Ok(vec![
            TrafficCounter {
                name: "outbound>>>proxy>>>traffic>>>uplink".into(),
                value: 11,
            },
            TrafficCounter {
                name: "outbound>>>proxy>>>traffic>>>downlink".into(),
                value: 22,
            },
        ])
    }
}

#[test]
fn status_and_stats_endpoints_expose_traffic() {
    let (api, xray) = env_with_stats_and_adapter(Arc::new(FakeStats), Box::new(RunningAdapter));
    let node = XrayNode {
        id: "node-1".into(),
        subscription_id: "sub-1".into(),
        name: "Test".into(),
        country_code: String::new(),
        country_name: String::new(),
        latitude: None,
        longitude: None,
        protocol: pm_xray::Protocol::Vless,
        address: "vless.example.com".into(),
        port: 443,
        raw_config: "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@vless.example.com:443"
            .into(),
    };
    xray.manager
        .connect(&node, 1080, InboundProtocol::Socks)
        .unwrap();

    let response = api.handle(request("GET", "/api/v1/xray/status", "", Vec::new()));
    assert_eq!(response.status, 200, "{}", response.body_string());
    let status: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(status["traffic"]["uplink"], 11);
    assert_eq!(status["traffic"]["downlink"], 22);

    let response = api.handle(request("GET", "/api/v1/xray/stats", "name=proxy", Vec::new()));
    assert_eq!(response.status, 200, "{}", response.body_string());
    let stats: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(stats["available"], true);
    assert_eq!(stats["name"], "proxy");
    assert_eq!(stats["node"]["uplink"], 11);
    assert_eq!(stats["node"]["downlink"], 22);
    assert_eq!(stats["total"]["uplink"], 11);
    assert_eq!(stats["total"]["downlink"], 22);
    assert_eq!(stats["counters"].as_array().unwrap().len(), 2);
    assert!(stats["error"].is_null());
}

#[test]
fn stats_endpoint_reports_noop_fallback() {
    let (api, _) = env();
    let response = api.handle(request("GET", "/api/v1/xray/stats", "", Vec::new()));
    assert_eq!(response.status, 200, "{}", response.body_string());
    let stats: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(stats["available"], false);
    assert_eq!(stats["total"]["uplink"], 0);
    assert_eq!(stats["total"]["downlink"], 0);

    let status: serde_json::Value = serde_json::from_slice(
        &api.handle(request("GET", "/api/v1/xray/status", "", Vec::new()))
            .body,
    )
    .unwrap();
    assert!(status.get("traffic").is_none());
}

#[test]
fn api_never_exposes_node_secrets_or_subscription_credentials() {
    use pm_xray::{AddSubscriptionRequest, XrayNode};

    let (api, xray) = env();
    xray.store
        .add(&AddSubscriptionRequest {
            name: String::new(),
            url: "https://user:secret@example.com/sub?header-X-Key=token".into(),
            enabled: false,
        })
        .unwrap();
    let node = XrayNode {
        id: "n1".into(),
        subscription_id: "s1".into(),
        name: "n".into(),
        country_code: "DE".into(),
        country_name: "Germany".into(),
        latitude: None,
        longitude: None,
        protocol: pm_xray::Protocol::Vless,
        address: "example.com".into(),
        port: 443,
        raw_config: "vless://uuid:password@example.com:443".into(),
    };
    xray.store.replace_nodes("s1", &[node]).unwrap();

    let response = api.handle(request("GET", "/api/v1/xray/nodes", "", Vec::new()));
    let body = response.body_string();
    assert!(!body.contains("uuid:password"), "{body}");
    assert!(!body.contains("raw_config"), "{body}");

    let response = api.handle(request("GET", "/api/v1/xray/subscriptions", "", Vec::new()));
    let body = response.body_string();
    assert!(!body.contains("secret"), "{body}");
    assert!(!body.contains("header-X-Key"), "{body}");
}

#[test]
fn new_subscription_is_enabled_by_default() {
    let req: AddSubscriptionRequest =
        serde_json::from_str(r#"{"url":"https://example.com/sub"}"#).unwrap();
    assert!(req.enabled);
}

#[test]
fn manual_nodes_endpoint_adds_direct_link() {
    let (api, xray) = env();
    let body = br#"{"url":"vless://00010203-0405-0607-0809-0a0b0c0d0e0f@manual.example.com:443#DE-Manual","name":"Custom DE"}"#;
    let response = api.handle(request("POST", "/api/v1/xray/nodes/manual", "", body.to_vec()));
    assert_eq!(response.status, 200, "{}", response.body_string());

    let nodes = xray.store.list_nodes().unwrap();
    assert_eq!(nodes.len(), 1);
    assert_eq!(nodes[0].country_code, "DE");
    assert_eq!(nodes[0].address, "manual.example.com");
}

