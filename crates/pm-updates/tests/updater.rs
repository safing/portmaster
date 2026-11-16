//! Tests for the updates module flow (Go `service/updates/module.go`).

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use pm_notifications::Notifications;
use pm_updates::{
    Artifact, HttpClient, Index, UpdateError, Updater, UpdaterConfig,
    NO_NEW_UPDATE_NOTIFICATION_ID, RESTART_REQUIRED_NOTIFICATION_ID,
    UPDATE_AVAILABLE_NOTIFICATION_ID,
};

static COUNTER: AtomicU32 = AtomicU32::new(0);

const INDEX_URL: &str = "https://updates.example.com/index.json";
const ARTIFACT_URL: &str = "https://updates.example.com/tool";
const ARTIFACT_CONTENT: &[u8] = b"new tool content";

struct TempTree {
    root: PathBuf,
}

impl TempTree {
    fn new(prefix: &str) -> TempTree {
        let root = std::env::temp_dir().join(format!(
            "pm-updates-updater-{prefix}-{}-{}",
            std::process::id(),
            COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir_all(&root).expect("create temp tree");
        TempTree { root }
    }

    fn path(&self, name: &str) -> PathBuf {
        self.root.join(name)
    }

    fn path_str(&self, name: &str) -> String {
        self.path(name).to_string_lossy().into_owned()
    }
}

impl Drop for TempTree {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.root);
    }
}

struct FakeClient {
    files: HashMap<String, Vec<u8>>,
}

impl FakeClient {
    fn with_index(index: &Index) -> FakeClient {
        FakeClient {
            files: HashMap::from([
                (INDEX_URL.to_string(), index.to_json().expect("index json")),
                (ARTIFACT_URL.to_string(), ARTIFACT_CONTENT.to_vec()),
            ]),
        }
    }
}

impl HttpClient for FakeClient {
    fn get(&self, url: &str) -> Result<Vec<u8>, String> {
        self.files
            .get(url)
            .cloned()
            .ok_or_else(|| format!("unknown URL {url}"))
    }
}

fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64
}

fn sha256(content: &[u8]) -> String {
    use sha2::Digest;
    hex::encode(sha2::Sha256::digest(content))
}

fn index(version: &str, published: i64) -> Index {
    Index {
        name: "test".to_string(),
        version: version.to_string(),
        published,
        artifacts: vec![Artifact {
            filename: "tool".to_string(),
            sha256: sha256(ARTIFACT_CONTENT),
            urls: vec![ARTIFACT_URL.to_string()],
            ..Artifact::default()
        }],
        version_num: pm_updates::Version::parse(version).ok(),
        is_locally_generated: false,
    }
}

fn base_config(tree: &TempTree) -> UpdaterConfig {
    UpdaterConfig {
        name: "test".to_string(),
        directory: tree.path_str("bin"),
        download_directory: tree.path_str("download"),
        purge_directory: tree.path_str("purge"),
        index_file: "index.json".to_string(),
        index_urls: vec![INDEX_URL.to_string()],
        auto_check: true,
        auto_download: true,
        auto_apply: true,
        notify: true,
        ..UpdaterConfig::default()
    }
}

fn updater(
    config: UpdaterConfig,
    index: &Index,
    notifications: Arc<Notifications>,
) -> Updater {
    Updater::new(
        config,
        Arc::new(FakeClient::with_index(index)),
        "1.0.0",
        Some(notifications),
        None,
    )
    .expect("create updater")
}

#[test]
fn auto_update_installs_the_new_version() {
    let tree = TempTree::new("auto");
    let notifications = Arc::new(Notifications::new());
    let update = index("2.0.0", now() - 60);
    let updater = updater(base_config(&tree), &update, Arc::clone(&notifications));
    let mut events = updater.subscribe_resource_updates();
    updater.start();

    updater.update_check().expect("update check");

    assert_eq!(
        std::fs::read(tree.path("bin/tool")).expect("installed artifact"),
        ARTIFACT_CONTENT
    );
    assert!(tree.path("bin/index.json").exists());
    assert!(!tree.path("download").exists(), "download dir cleaned up");
    assert!(!tree.path("purge").exists(), "purge dir cleaned up");
    assert_eq!(updater.get_index().expect("index").version, "2.0.0");
    let file = updater.get_file("tool").expect("artifact");
    assert_eq!(file.path(), tree.path_str("bin/tool"));
    assert!(events.try_recv().is_ok(), "resource update event emitted");
    assert!(
        notifications.get(NO_NEW_UPDATE_NOTIFICATION_ID).is_none(),
        "no up-to-date notification on success"
    );
}

#[test]
fn same_index_reports_up_to_date() {
    let tree = TempTree::new("same");
    let notifications = Arc::new(Notifications::new());
    let update = index("2.0.0", now() - 60);
    let updater = updater(base_config(&tree), &update, Arc::clone(&notifications));

    updater.update_check().expect("first update");
    // The cached index now has the same version and publish date.
    updater.update_check().expect("second update");

    let notification = notifications
        .get(NO_NEW_UPDATE_NOTIFICATION_ID)
        .expect("up-to-date notification")
        .snapshot();
    assert_eq!(notification.title, "Portmaster Is Up-To-Date");
    assert_eq!(
        notification.message,
        "Portmaster v2.0.0 is the newest version."
    );
    assert_eq!(notification.available_actions[0].id, "ack");
}

#[test]
fn update_without_auto_download_requires_action() {
    let tree = TempTree::new("action");
    let notifications = Arc::new(Notifications::new());
    let update = index("2.0.0", now() - 60);
    let mut config = base_config(&tree);
    config.auto_download = false;
    config.auto_apply = false;
    let updater = updater(config, &update, Arc::clone(&notifications));

    let error = updater.update_check().expect_err("action required");
    assert!(
        error.to_string().contains("updating failed: action required"),
        "{error}"
    );
    assert!(!tree.path("bin/tool").exists(), "nothing was downloaded");

    let notification = notifications
        .get(UPDATE_AVAILABLE_NOTIFICATION_ID)
        .expect("update notification")
        .snapshot();
    assert_eq!(notification.title, "New Update Available");
    assert_eq!(
        notification.message,
        "Portmaster v2.0.0 is available. Click Upgrade to download and upgrade now."
    );
    assert_eq!(
        notification
            .available_actions
            .iter()
            .map(|action| action.id.as_str())
            .collect::<Vec<_>>(),
        vec!["ack", "upgrade"]
    );
    let upgrade = &notification.available_actions[1];
    assert_eq!(upgrade.type_, pm_notifications::ACTION_TYPE_WEBHOOK);
    assert_eq!(upgrade.payload["URL"], "updates/apply");
    assert_eq!(upgrade.payload["Method"], "POST");
}

#[test]
fn force_update_ignores_the_auto_flags() {
    let tree = TempTree::new("force");
    let notifications = Arc::new(Notifications::new());
    let update = index("2.0.0", now() - 60);
    let mut config = base_config(&tree);
    config.auto_download = false;
    config.auto_apply = false;
    let updater = updater(config, &update, Arc::clone(&notifications));

    updater.force_update().expect("forced update");

    assert_eq!(
        std::fs::read(tree.path("bin/tool")).expect("installed artifact"),
        ARTIFACT_CONTENT
    );
}

#[test]
fn restart_required_notifies_or_restarts() {
    // Notify: the user has to restart.
    let tree = TempTree::new("restart-notify");
    let notifications = Arc::new(Notifications::new());
    let update = index("2.0.0", now() - 60);
    let mut config = base_config(&tree);
    config.needs_restart = true;
    let updater = updater(config, &update, Arc::clone(&notifications));

    let error = updater.update_check().expect_err("action required");
    assert!(
        error.to_string().contains("updating failed: action required"),
        "{error}"
    );
    let notification = notifications
        .get(RESTART_REQUIRED_NOTIFICATION_ID)
        .expect("restart notification")
        .snapshot();
    assert_eq!(notification.title, "Restart Required");
    assert_eq!(
        notification.message,
        "Portmaster v2.0.0 is installed. Restart to use new version."
    );
    assert_eq!(notification.available_actions[1].id, "restart");
    assert_eq!(
        notification.available_actions[1].payload["URL"],
        "core/restart?source=upgrade"
    );

    // No notify: restart immediately through the callback.
    let tree = TempTree::new("restart-auto");
    let notifications = Arc::new(Notifications::new());
    let update = index("2.0.0", now() - 60);
    let mut config = base_config(&tree);
    config.needs_restart = true;
    config.notify = false;
    let restarted = Arc::new(Mutex::new(false));
    let restarted_clone = Arc::clone(&restarted);
    let updater = Updater::new(
        config,
        Arc::new(FakeClient::with_index(&update)),
        "1.0.0",
        Some(notifications),
        Some(Arc::new(move || {
            *restarted_clone.lock().expect("lock") = true;
        })),
    )
    .expect("updater");

    updater.update_check().expect("update check");
    assert!(*restarted.lock().expect("lock"), "restart was triggered");
}

#[test]
fn corrupted_installation_is_repaired() {
    let tree = TempTree::new("repair");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");
    std::fs::write(tree.path("bin/tool"), b"corrupted").expect("write corrupt file");

    let notifications = Arc::new(Notifications::new());
    let update = index("2.0.0", now() - 60);
    // The main directory claims the correct checksum, but the file is corrupt.
    std::fs::write(
        tree.path("bin/index.json"),
        update.to_json().expect("index json"),
    )
    .expect("write index");

    let updater = updater(base_config(&tree), &update, Arc::clone(&notifications));
    assert!(
        updater
            .corrupted_installation()
            .expect("corruption")
            .contains("invalid artifact"),
    );

    updater.start();
    assert_eq!(updater.states().export().states.len(), 1);

    updater.update_check().expect("repair update");

    assert_eq!(
        std::fs::read(tree.path("bin/tool")).expect("repaired file"),
        ARTIFACT_CONTENT
    );
    assert_eq!(updater.corrupted_installation(), None);
    assert!(updater.states().export().states.is_empty());
}

#[test]
fn index_falls_back_to_a_directory_scan() {
    let tree = TempTree::new("scan");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");
    std::fs::write(tree.path("bin/existing"), b"existing").expect("write file");

    let updater = updater(
        base_config(&tree),
        &index("2.0.0", now() - 60),
        Arc::new(Notifications::new()),
    );

    let index = updater.get_index().expect("scanned index");
    assert_eq!(index.version, "1.0.0", "core version of the scan");
    assert!(index.artifacts.iter().any(|artifact| artifact.filename == "existing"));
    assert!(matches!(updater.get_file("missing"), Err(UpdateError::NotFound)));
}

#[test]
fn configure_updates_the_flags() {
    let tree = TempTree::new("configure");
    let update = index("2.0.0", now() - 60);
    let updater = updater(
        base_config(&tree),
        &update,
        Arc::new(Notifications::new()),
    );

    assert!(!updater.configure(true, vec![INDEX_URL.to_string()]));
    assert!(updater.configure(false, vec![INDEX_URL.to_string()]));
    assert!(!updater.configure(false, vec![INDEX_URL.to_string()]));
    assert!(updater.configure(false, vec!["https://other/index.json".to_string()]));
    assert_eq!(
        updater.config().index_urls,
        vec!["https://other/index.json".to_string()]
    );
}

#[test]
fn repeat_duration_matches_go() {
    assert_eq!(pm_updates::UPDATE_TASK_REPEAT_DURATION, Duration::from_secs(3600));
}
