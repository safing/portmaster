//! Format, API and persistence tests for pm-metrics.

use std::collections::BTreeMap;
use std::sync::Arc;

use pm_api::{Api, AuthToken, Request, PERMIT_ADMIN, PERMIT_ANYONE, PERMIT_SELF, PERMIT_USER};
use pm_config::EXPERTISE_LEVEL_EXPERT;
use pm_database::{DatabaseInfo, Databases};
use pm_metrics::{FetchFn, GaugeFn, Options, Registry};

fn labels(entries: &[(&str, &str)]) -> BTreeMap<String, String> {
    entries
        .iter()
        .map(|(k, v)| ((*k).to_string(), (*v).to_string()))
        .collect()
}

/// Golden output produced by Go VictoriaMetrics v1.40.2 with the same metrics.
const GOLDEN: &str = "\
portmaster_host_mem_pct2{instance=\"GachyOS\"} 12\n\
portmaster_logs_warn_total2{instance=\"GachyOS\"} 7\n\
portmaster_net_conns_total{blocked=\"true\",direction=\"out\",instance=\"GachyOS\"} 5\n\
portmaster_req_duration_bucket{instance=\"GachyOS\",vmrange=\"0...1.000e-09\"} 1\n\
portmaster_req_duration_bucket{instance=\"GachyOS\",vmrange=\"4.642e-01...5.275e-01\"} 1\n\
portmaster_req_duration_bucket{instance=\"GachyOS\",vmrange=\"8.799e-01...1.000e+00\"} 1\n\
portmaster_req_duration_bucket{instance=\"GachyOS\",vmrange=\"2.448e+00...2.783e+00\"} 1\n\
portmaster_req_duration_sum{instance=\"GachyOS\"} 4.000000000001\n\
portmaster_req_duration_count{instance=\"GachyOS\"} 4\n";

fn golden_registry() -> Registry {
    let registry = Registry::new();
    registry.set_namespace("portmaster").unwrap();
    registry.add_global_label("instance", "GachyOS").unwrap();
    registry.mark_ready();

    let counter = registry
        .new_counter(
            "net/conns/total",
            Some(labels(&[("direction", "out"), ("blocked", "true")])),
            Some(Options {
                name: "Connections".to_string(),
                internal_id: "blocked_out".to_string(),
                permission: PERMIT_USER,
                ..Options::default()
            }),
        )
        .unwrap();
    counter.inc();
    counter.add(4);

    registry
        .new_gauge(
            "host/mem/pct2",
            None,
            Some(GaugeFn::new(|| 12.0)),
            Some(Options {
                permission: PERMIT_USER,
                ..Options::default()
            }),
        )
        .unwrap();
    registry
        .new_fetching_counter(
            "logs/warn/total2",
            None,
            FetchFn::new(|| 7),
            Some(Options {
                permission: PERMIT_USER,
                ..Options::default()
            }),
        )
        .unwrap();
    let histogram = registry
        .new_histogram(
            "req/duration",
            None,
            Some(Options {
                permission: PERMIT_USER,
                expertise_level: EXPERTISE_LEVEL_EXPERT,
                ..Options::default()
            }),
        )
        .unwrap();
    for value in [0.5, 1.0, 2.5, 1e-12] {
        histogram.update(value);
    }
    registry
}

fn write(registry: &Registry, permission: i8, expertise: u8) -> String {
    let mut out = String::new();
    registry.write_prometheus(&mut out, permission, expertise);
    out
}

#[test]
fn prometheus_format_matches_go_reference() {
    let registry = golden_registry();
    assert_eq!(write(&registry, pm_api::PERMIT_SELF, 2), GOLDEN);
}

#[test]
fn permission_and_expertise_filtering() {
    let registry = golden_registry();
    // Developer is enough for everything; user hides the expert histogram.
    assert!(write(&registry, PERMIT_USER, 0).contains("net_conns_total"));
    assert!(!write(&registry, PERMIT_USER, 0).contains("req_duration"));
    // Anyone cannot see PermitUser metrics.
    assert!(!write(&registry, PERMIT_ANYONE, 2).contains("net_conns_total"));
    // Higher permission always sees everything.
    assert!(write(&registry, PERMIT_ADMIN, 2).contains("req_duration_bucket"));
}

#[test]
fn duplicate_and_early_registration_are_rejected() {
    let registry = Registry::new();
    assert!(matches!(
        registry.new_counter("early/metric", None, None),
        Err(pm_metrics::Error::TooEarly(_))
    ));
    registry.mark_ready();
    registry.new_counter("a/metric", None, None).unwrap();
    assert_eq!(
        registry.new_counter("a/metric", None, None).err().unwrap(),
        pm_metrics::Error::AlreadyRegistered
    );
    let opts = Options {
        internal_id: "same_id".to_string(),
        ..Options::default()
    };
    registry.new_counter("b/metric", None, Some(opts.clone())).unwrap();
    assert_eq!(
        registry.new_counter("c/metric", None, Some(opts)).err().unwrap(),
        pm_metrics::Error::AlreadyRegisteredInternalId
    );
}

#[test]
fn namespace_and_labels_are_frozen_after_first_metric() {
    let registry = Registry::new();
    registry.set_namespace("ns").unwrap();
    assert_eq!(
        registry.set_namespace("other").err().unwrap(),
        pm_metrics::Error::AlreadySet
    );
    registry.add_global_label("instance", "x").unwrap();
    registry.mark_ready();
    registry.new_counter("c", None, None).unwrap();
    assert_eq!(
        registry.set_namespace("other").err().unwrap(),
        pm_metrics::Error::AlreadyStarted
    );
    assert_eq!(
        registry.add_global_label("k", "v").err().unwrap(),
        pm_metrics::Error::AlreadyStarted
    );
}

#[test]
fn api_endpoints_and_export() {
    let registry = Arc::new(golden_registry());
    let api = Api::new("127.0.0.1:0");
    api.set_dev_mode(true);
    pm_metrics::api::register_api(&api, Arc::clone(&registry), Arc::new(|| {})).unwrap();

    let mut request = Request::new("GET", "/api/v1/metrics/list");
    request.auth = Some(AuthToken::full());
    let response = api.handle(request);
    assert_eq!(response.status, 200);
    let list = response.body_string();
    assert!(list.contains("\"Identifier\":\"net/conns/total\""));
    assert!(list.contains("\"LabeledIdentifier\""));
    assert!(list.contains("\"CurrentValue\":5"));

    let mut request = Request::new("GET", "/api/v1/metrics/values");
    request.auth = Some(AuthToken::full());
    let response = api.handle(request);
    assert_eq!(response.status, 200);
    let values: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(values["blocked_out"], 5);
    assert!(values.get("portmaster_logs_warn_total2{instance=\"GachyOS\"}").is_some());

    let mut request = Request::new("GET", "/api/v1/metrics/values").with_query("internal-only");
    request.auth = Some(AuthToken::full());
    let response = api.handle(request);
    let internal: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(internal, serde_json::json!({"blocked_out": 5}));

    let mut request = Request::new("GET", "/metrics");
    request.auth = Some(AuthToken::full());
    let response = api.handle(request);
    assert_eq!(response.status, 200);
    assert_eq!(
        response.headers.get("content-type").unwrap(),
        "text/plain; version=0.0.4; charset=utf-8"
    );
    assert_eq!(response.body_string(), GOLDEN);
}

fn temp_databases(tag: &str) -> (Arc<Databases>, std::path::PathBuf) {
    static COUNTER: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    let unique = format!(
        "pm-metrics-{tag}-{}-{}",
        std::process::id(),
        COUNTER.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
    );
    let dir = std::env::temp_dir().join(unique);
    let databases = Arc::new(Databases::new(&dir).unwrap());
    databases
        .register(DatabaseInfo {
            name: "core".to_string(),
            description: String::new(),
            storage_type: "sqlite".to_string(),
            shadow_delete: false,
        })
        .unwrap();
    databases.controller("core").unwrap();
    (databases, dir)
}

#[test]
fn persistence_round_trip() {
    let key = "core:metrics/storage";
    let (databases, dir) = temp_databases("persist");

    let registry = Registry::new();
    registry.mark_ready();
    registry
        .enable_persistence(databases.internal_interface(), key)
        .unwrap();
    let opts = Options {
        persist: true,
        ..Options::default()
    };
    let counter = registry.new_counter("persisted/counter", None, Some(opts.clone())).unwrap();
    counter.add(41);
    registry.store_persistent_metrics();

    let reloaded = Registry::new();
    reloaded.mark_ready();
    reloaded
        .enable_persistence(databases.internal_interface(), key)
        .unwrap();
    let counter = reloaded.new_counter("persisted/counter", None, Some(opts)).unwrap();
    assert_eq!(counter.get(), 41);
    assert_eq!(write(&reloaded, PERMIT_SELF, 2), "persisted_counter 41\n");

    // A non-persistent counter is not restored.
    let non_persistent = reloaded.new_counter("other/counter", None, None).unwrap();
    non_persistent.add(7);
    assert_eq!(non_persistent.get(), 7);

    databases.shutdown().unwrap();
    let _ = std::fs::remove_dir_all(dir);
}

#[test]
fn persistence_enabled_only_once() {
    let (databases, dir) = temp_databases("once");
    let registry = Registry::new();
    registry
        .enable_persistence(databases.internal_interface(), "core:metrics/storage")
        .unwrap();
    assert_eq!(
        registry
            .enable_persistence(databases.internal_interface(), "core:metrics/storage")
            .err()
            .unwrap(),
        pm_metrics::Error::AlreadyInitialized
    );
    databases.shutdown().unwrap();
    let _ = std::fs::remove_dir_all(dir);
}

#[test]
fn invalid_names_are_rejected() {
    let registry = Registry::new();
    registry.mark_ready();
    assert!(registry.new_counter("bad-name", None, None).is_err());
    assert!(registry
        .new_counter("ok", Some(labels(&[("bad-label", "x")])), None)
        .is_err());
}
