//! Integration tests for the broadcasts port.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicI64, AtomicU64, Ordering};
use std::sync::Arc;

use pm_api::{Api, AuthError, Authenticator, Request, PERMIT_ADMIN};
use pm_broadcasts::{
    make_numeric_version, parse_broadcast_source, BroadcastStates, BuildVersion, DirectoryFileSource,
    LocationData, MatchingEnvironment, NoopAttachment, ResourceVersion, SimpleVersions, Broadcasts,
    BROADCAST_NOTIFICATION_ID_PREFIX, BROADCAST_STATES_DB_KEY,
};
use pm_database::{DatabaseInfo, Databases, Options};
use pm_notifications::Notifications;

static TEST_COUNTER: AtomicU64 = AtomicU64::new(0);

struct TestEnvironment {
    now: AtomicI64,
}

impl TestEnvironment {
    fn new(now: i64) -> TestEnvironment {
        TestEnvironment {
            now: AtomicI64::new(now),
        }
    }

    fn advance(&self, seconds: i64) {
        self.now.fetch_add(seconds, Ordering::SeqCst);
    }
}

impl MatchingEnvironment for TestEnvironment {
    fn versions(&self) -> SimpleVersions {
        let mut resources = std::collections::BTreeMap::new();
        resources.insert(
            "test-resource".to_string(),
            ResourceVersion {
                version: "1.2.3".to_string(),
            },
        );
        SimpleVersions {
            build: BuildVersion {
                version: "0.9.0".to_string(),
            },
            resources,
            channel: "beta".to_string(),
        }
    }

    fn config_values(&self) -> serde_json::Map<String, serde_json::Value> {
        let mut values = serde_json::Map::new();
        values.insert(
            "core/releaseChannel".to_string(),
            serde_json::Value::String("beta".to_string()),
        );
        values
    }

    fn location(&self) -> Option<LocationData> {
        Some(LocationData {
            country: "US".to_string(),
            asn: 1234,
            as_org: "Test ASN".to_string(),
            source: "interface".to_string(),
            source_accuracy: 6,
            ..LocationData::default()
        })
    }

    fn now(&self) -> i64 {
        self.now.load(Ordering::SeqCst)
    }

    fn local_offset_seconds(&self) -> i32 {
        0
    }
}

struct Harness {
    module: Arc<Broadcasts>,
    notifications: Arc<Notifications>,
    databases: Arc<Databases>,
    env: Arc<TestEnvironment>,
}

fn harness(now: i64) -> Harness {
    let counter = TEST_COUNTER.fetch_add(1, Ordering::SeqCst);
    let dir = std::env::temp_dir().join(format!(
        "portmaster-broadcasts-test-{}-{}",
        std::process::id(),
        counter
    ));
    let _ = std::fs::remove_dir_all(&dir);
    let databases = Arc::new(Databases::new(&dir).unwrap());
    databases
        .register(DatabaseInfo {
            name: "core".to_string(),
            description: "core test database".to_string(),
            storage_type: "sqlite".to_string(),
            shadow_delete: false,
        })
        .unwrap();
    let db = databases.interface(Options {
        local: true,
        internal: true,
        ..Options::default()
    });
    let notifications = Arc::new(Notifications::new());
    let env = Arc::new(TestEnvironment::new(now));
    let module = Arc::new(Broadcasts::new(
        db,
        Arc::clone(&notifications),
        env.clone(),
        Arc::new(DirectoryFileSource::new("/nonexistent")),
        Arc::new(NoopAttachment),
    ));
    Harness {
        module,
        notifications,
        databases,
        env,
    }
}

fn yaml(body: &str) -> Vec<u8> {
    format!("notifications:\n{body}").into_bytes()
}

fn line(id: &str, text: &str) -> String {
    format!("{id:>30}: {text}")
}

#[test]
fn parses_go_testdata_yaml() {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../service/broadcasts/testdata/notifications.yaml");
    let data = std::fs::read(&path).unwrap();
    let source = parse_broadcast_source(&data).unwrap();
    let broadcast = &source.notifications["new-network-monitor-feedback"];
    assert_eq!(broadcast.notification.title, "New: Full-Device Network Activity");
    assert_eq!(
        broadcast.match_,
        "Version sameas 0.9.0 and Updates.Channel sameas beta"
    );
    assert!(broadcast.matching_query.is_some());
    assert_eq!(broadcast.repeat_duration.as_secs(), 0);
}

#[test]
fn matching_broadcast_is_displayed_with_ack_action() {
    let h = harness(1_700_000_000);
    let source = yaml(
        "  match-and-show:\n    title: Hello\n    message: World\n    match: \"Version sameas 0.9.0 and Updates.Channel sameas beta\"\n",
    );
    let result = h.module.simulate(&source, false).unwrap();
    assert_eq!(result, line("match-and-show", "displayed"));

    let notification = h
        .notifications
        .get(&format!("{BROADCAST_NOTIFICATION_ID_PREFIX}match-and-show"))
        .unwrap();
    let snapshot = notification.snapshot();
    assert_eq!(snapshot.title, "Hello");
    assert_eq!(snapshot.message, "World");
    assert_eq!(snapshot.available_actions.len(), 1);
    assert_eq!(snapshot.available_actions[0].id, "ack");
    assert_eq!(snapshot.available_actions[0].text, "Got it!");
}

#[test]
fn non_matching_broadcast_is_skipped() {
    let h = harness(1_700_000_000);
    let source = yaml(
        "  no-match:\n    title: Nope\n    message: Nope\n    match: \"Version sameas 9.9.9\"\n",
    );
    let result = h.module.simulate(&source, false).unwrap();
    assert_eq!(
        result,
        line("no-match", "broadcast skipped: does not match")
    );
    assert!(h
        .notifications
        .get(&format!("{BROADCAST_NOTIFICATION_ID_PREFIX}no-match"))
        .is_none());
}

#[test]
fn permanent_broadcast_has_no_ack_action() {
    let h = harness(1_700_000_000);
    let source = yaml(
        "  permanent:\n    title: Notice\n    message: Cannot dismiss\n    permanent: true\n    type: 1\n",
    );
    let result = h.module.simulate(&source, false).unwrap();
    assert_eq!(result, line("permanent", "displayed"));
    let notification = h
        .notifications
        .get(&format!("{BROADCAST_NOTIFICATION_ID_PREFIX}permanent"))
        .unwrap();
    assert_eq!(notification.snapshot().type_, 1);
    assert!(notification.snapshot().available_actions.is_empty());
}

#[test]
fn already_active_broadcast_is_skipped() {
    let h = harness(1_700_000_000);
    let source = yaml("  active:\n    title: A\n    message: B\n");
    assert_eq!(h.module.simulate(&source, false).unwrap(), line("active", "displayed"));
    assert_eq!(
        h.module.simulate(&source, false).unwrap(),
        line("active", "broadcast skipped: already active")
    );
}

#[test]
fn remove_deletes_notification_and_state() {
    let h = harness(1_700_000_000);
    let source = yaml("  removable:\n    title: A\n    message: B\n");
    assert_eq!(h.module.simulate(&source, false).unwrap(), line("removable", "displayed"));
    let key = format!("{BROADCAST_NOTIFICATION_ID_PREFIX}removable");

    let remove_source = yaml("  removable:\n    title: A\n    message: B\n    remove: true\n");
    assert_eq!(
        h.module.simulate(&remove_source, false).unwrap(),
        line("removable", "broadcast skipped: removed by source")
    );
    assert!(h.notifications.get(&key).is_none());

    // A later match with a different ID is now reported as a plain mismatch.
    let mismatch_source = yaml(
        "  removable:\n    title: A\n    message: B\n    match: \"Version sameas 9.9.9\"\n",
    );
    assert_eq!(
        h.module.simulate(&mismatch_source, false).unwrap(),
        line("removable", "broadcast skipped: does not match")
    );
}

#[test]
fn acknowledgement_persists_state_and_respects_minimum_repeat() {
    let h = harness(1_700_000_000);
    let source = yaml(
        "  repeated:\n    title: A\n    message: B\n    repeat: 1m\n",
    );
    assert_eq!(h.module.simulate(&source, false).unwrap(), line("repeated", "displayed"));
    let event_id = format!("{BROADCAST_NOTIFICATION_ID_PREFIX}repeated");

    assert!(h.notifications.select_action(&event_id, "ack"));
    let states = BroadcastStates::get(&h.databases.interface(Options {
        local: true,
        internal: true,
        ..Options::default()
    }))
    .unwrap();
    assert_eq!(states.states["repeated"].read, 1_700_000_000);
    assert!(h.notifications.get(&event_id).is_none());

    // 1m is raised to the 1h minimum, so it is still too soon.
    h.env.advance(60);
    assert_eq!(
        h.module.simulate(&source, true).unwrap(),
        line("repeated", "broadcast skipped: already shown")
    );

    // After more than an hour it is shown again.
    h.env.advance(3600);
    assert_eq!(
        h.module.simulate(&source, true).unwrap(),
        line("repeated", "displayed")
    );
}

#[test]
fn reset_state_removes_broadcast_state() {
    let h = harness(1_700_000_000);
    let source = yaml("  to-ack:\n    title: A\n    message: B\n");
    h.module.simulate(&source, false).unwrap();
    h.notifications
        .select_action(&format!("{BROADCAST_NOTIFICATION_ID_PREFIX}to-ack"), "ack");

    let message = h.module.reset_state().unwrap();
    assert_eq!(
        message,
        "Reset complete. Some notifications require a restart to reappear."
    );
    assert!(h
        .databases
        .interface(Options {
            local: true,
            internal: true,
            ..Options::default()
        })
        .get(BROADCAST_STATES_DB_KEY)
        .is_err());
    // Resetting twice is fine, like Go.
    assert!(h.module.reset_state().is_ok());
}

#[test]
fn api_endpoints_are_registered_and_work() {
    struct AdminAuth;
    impl Authenticator for AdminAuth {
        fn authenticate(&self, _request: &Request) -> Result<Option<pm_api::AuthToken>, AuthError> {
            Ok(Some(pm_api::AuthToken::new(PERMIT_ADMIN, PERMIT_ADMIN)))
        }
    }

    let h = harness(1_700_000_000);
    let api = Api::new("127.0.0.1:0");
    api.set_authenticator(Arc::new(AdminAuth));
    h.module.register_api(&api).unwrap();

    // matching-data returns the collected JSON.
    let response = api.handle(
        Request::new("GET", "/api/v1/broadcasts/matching-data").with_host("host"),
    );
    assert_eq!(response.status, 200);
    let data: serde_json::Value = serde_json::from_slice(&response.body).unwrap();
    assert_eq!(data["Version"], "0.9.0");
    assert_eq!(data["Updates"]["Channel"], "beta");
    assert_eq!(data["NumericVersion"], 9_000);
    assert_eq!(data["UptimeHours"], 0);
    assert_eq!(data["Config"]["core/releaseChannel"], "beta");
    assert_eq!(data["Location"]["Country"], "US");
    assert_eq!(data["Current"]["UnixTime"], 1_700_000_000);

    // simulate accepts the YAML in the request body.
    let source = yaml("  via-api:\n    title: A\n    message: B\n");
    let response = api.handle(
        Request::new("POST", "/api/v1/broadcasts/simulate")
            .with_host("host")
            .with_query("state=false")
            .with_body(source),
    );
    assert_eq!(response.status, 200);
    assert_eq!(response.body_string().trim_end(), line("via-api", "displayed"));

    // reset-state returns the Go message.
    let response = api.handle(
        Request::new("POST", "/api/v1/broadcasts/reset-state").with_host("host"),
    );
    assert_eq!(response.status, 200);
    assert_eq!(
        response.body_string().trim_end(),
        "Reset complete. Some notifications require a restart to reappear."
    );
}

#[test]
fn run_once_loads_yaml_from_file_source() {
    let counter = TEST_COUNTER.fetch_add(1, Ordering::SeqCst);
    let dir = std::env::temp_dir().join(format!(
        "portmaster-broadcasts-run-{}-{}",
        std::process::id(),
        counter
    ));
    let _ = std::fs::remove_dir_all(&dir);
    std::fs::create_dir_all(&dir).unwrap();
    let resource = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../service/broadcasts/testdata/notifications.yaml");
    std::fs::copy(resource, dir.join("notifications.yaml")).unwrap();

    let databases = Arc::new(Databases::new(dir.join("db")).unwrap());
    databases
        .register(DatabaseInfo {
            name: "core".to_string(),
            description: "core test database".to_string(),
            storage_type: "sqlite".to_string(),
            shadow_delete: false,
        })
        .unwrap();
    let notifications = Arc::new(Notifications::new());
    let env = Arc::new(TestEnvironment::new(1_700_000_000));
    let module = Arc::new(Broadcasts::new(
        databases.interface(Options {
            local: true,
            internal: true,
            ..Options::default()
        }),
        Arc::clone(&notifications),
        env,
        Arc::new(DirectoryFileSource::new(&dir)),
        Arc::new(NoopAttachment),
    ));
    module.run_once().unwrap();
    assert!(notifications
        .get(&format!(
            "{BROADCAST_NOTIFICATION_ID_PREFIX}new-network-monitor-feedback"
        ))
        .is_some());
}

#[test]
fn numeric_version_matches_go() {
    assert_eq!(make_numeric_version("0.9.0").unwrap(), 9_000);
    assert_eq!(make_numeric_version("1.2.3").unwrap(), 1_002_003);
    assert!(make_numeric_version("garbage").is_err());
}

#[test]
fn attach_to_module_is_recorded() {
    // The recorder in pm-broadcasts keeps attached event IDs observable.
    let h = harness(1_700_000_000);
    let source = yaml(
        "  attached:\n    title: A\n    message: B\n    attachToModule: true\n",
    );
    h.module.simulate(&source, false).unwrap();
    assert_eq!(
        h.module.attached_notifications(),
        vec![format!("{BROADCAST_NOTIFICATION_ID_PREFIX}attached")]
    );
}
