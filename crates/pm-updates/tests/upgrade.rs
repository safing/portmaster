//! Tests for applying upgrades (Go `service/updates/upgrade.go`).

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU32, Ordering};

use pm_updates::{
    UpdateCommandConfig, UpgradeError, UpdaterConfig, Upgrader, Artifact, Index, Version,
};

static COUNTER: AtomicU32 = AtomicU32::new(0);

struct TempTree {
    root: PathBuf,
}

impl TempTree {
    fn new(prefix: &str) -> TempTree {
        let root = std::env::temp_dir().join(format!(
            "pm-updates-upgrade-{prefix}-{}-{}",
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

    fn write(&self, relative: &str, content: &[u8]) {
        let path = self.root.join(relative);
        std::fs::create_dir_all(path.parent().unwrap()).expect("create parent dir");
        std::fs::write(path, content).expect("write file");
    }
}

impl Drop for TempTree {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.root);
    }
}

fn config(tree: &TempTree) -> UpdaterConfig {
    UpdaterConfig {
        name: "test".to_string(),
        directory: tree.path_str("bin"),
        download_directory: tree.path_str("download"),
        purge_directory: tree.path_str("purge"),
        index_file: "index.json".to_string(),
        ..UpdaterConfig::default()
    }
}

fn index_with(version: &str, artifacts: Vec<Artifact>) -> Index {
    Index {
        name: "test".to_string(),
        version: version.to_string(),
        published: 1_700_000_000,
        artifacts,
        version_num: Version::parse(version).ok(),
        is_locally_generated: false,
    }
}

fn artifact(filename: &str, content: &[u8]) -> Artifact {
    Artifact {
        filename: filename.to_string(),
        sha256: hex_sha256(content),
        ..Artifact::default()
    }
}

fn hex_sha256(content: &[u8]) -> String {
    use sha2::Digest;
    hex::encode(sha2::Sha256::digest(content))
}

fn prepare_download(tree: &TempTree, artifacts: &[(&str, &[u8])]) {
    tree.write("download/index.json", b"{\"Name\":\"test\"}");
    for (name, content) in artifacts {
        tree.write(&format!("download/{name}"), content);
    }
}

#[test]
fn moves_the_new_version_in_and_the_old_one_to_purge() {
    let tree = TempTree::new("apply");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");
    tree.write("bin/old-tool", b"old");
    prepare_download(&tree, &[("tool", b"new")]);

    let downloaded = index_with("2.0.0", vec![artifact("tool", b"new")]);
    let upgrader = Upgrader::new(config(&tree));

    upgrader.upgrade(&downloaded, true).expect("upgrade");

    assert_eq!(
        std::fs::read(tree.path("bin/tool")).expect("new file"),
        b"new"
    );
    assert!(tree.path("bin/index.json").exists());
    assert!(!tree.path("bin/old-tool").exists());
    assert_eq!(
        std::fs::read(tree.path("purge/old-tool")).expect("purged file"),
        b"old"
    );
    assert_eq!(
        upgrader.index().expect("installed index").version,
        "2.0.0"
    );
}

#[cfg(unix)]
#[test]
fn sets_the_artifact_permissions() {
    use std::os::unix::fs::PermissionsExt;

    let tree = TempTree::new("modes");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");
    prepare_download(&tree, &[("tool", b"tool")]);

    let mut executable = artifact("tool", b"tool");
    executable.platform = pm_updates::go_platform();
    let mut data = artifact("data", b"data");
    data.platform = "other".to_string();

    let downloaded = index_with("2.0.0", vec![executable, data]);
    prepare_download(&tree, &[("data", b"data")]);
    Upgrader::new(config(&tree))
        .upgrade(&downloaded, true)
        .expect("upgrade");

    let mode = |name: &str| {
        std::fs::metadata(tree.path(name))
            .expect("metadata")
            .permissions()
            .mode()
            & 0o777
    };
    assert_eq!(mode("bin/tool"), 0o755);
    assert_eq!(mode("bin/data"), 0o644);
}

#[test]
fn identical_index_is_not_an_upgrade() {
    let tree = TempTree::new("same");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");

    let index = index_with("2.0.0", Vec::new());
    let upgrader = Upgrader::new(config(&tree));
    upgrader.set_index(Some(index.clone()));

    match upgrader.upgrade(&index, false) {
        Err(UpgradeError::NoUpdateAvailable) => {}
        other => panic!("expected no update available, got {other:?}"),
    }
}

#[test]
fn failed_upgrade_is_recovered() {
    let tree = TempTree::new("recover");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");
    tree.write("bin/old-tool", b"old");
    // The artifact is not in the download directory, so moving it fails.
    prepare_download(&tree, &[]);

    let downloaded = index_with("2.0.0", vec![artifact("missing", b"new")]);
    let upgrader = Upgrader::new(config(&tree));

    match upgrader.upgrade(&downloaded, true) {
        Err(UpgradeError::Recovered(message)) => {
            assert!(
                message.contains("failed to move file missing"),
                "unexpected message: {message}"
            );
        }
        other => panic!("expected recovery, got {other:?}"),
    }

    assert_eq!(
        std::fs::read(tree.path("bin/old-tool")).expect("old file back"),
        b"old"
    );
    assert!(upgrader.index().is_none(), "index was not installed");
}

#[test]
fn unfinished_files_are_deleted() {
    let tree = TempTree::new("unfinished");
    tree.write("download/left.download", b"partial");
    tree.write("download/left.copy", b"partial");
    tree.write("download/keep", b"data");
    std::fs::create_dir_all(tree.path("download/sub")).expect("create sub dir");

    let upgrader = Upgrader::new(config(&tree));
    upgrader
        .delete_unfinished_files(&tree.path("download"))
        .expect("delete unfinished");

    assert!(!tree.path("download/left.download").exists());
    assert!(!tree.path("download/left.copy").exists());
    assert!(tree.path("download/keep").exists());
    assert!(tree.path("download/sub").exists());
}

#[test]
fn cleanup_removes_purge_and_download_directories() {
    let tree = TempTree::new("cleanup");
    std::fs::create_dir_all(tree.path("purge")).expect("create purge");
    std::fs::create_dir_all(tree.path("download")).expect("create download");

    Upgrader::new(config(&tree))
        .cleanup_after_upgrade()
        .expect("cleanup");

    assert!(!tree.path("purge").exists());
    assert!(!tree.path("download").exists());
}

#[test]
fn failing_post_upgrade_command_fails_the_upgrade() {
    let tree = TempTree::new("command-fail");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");
    prepare_download(&tree, &[]);

    let mut config = config(&tree);
    config.post_upgrade_commands = vec![UpdateCommandConfig {
        command: "sh".to_string(),
        args: vec!["-c".to_string(), "echo boom; exit 3".to_string()],
        trigger_artifact_fname: String::new(),
        fail_on_error: true,
    }];

    let downloaded = index_with("2.0.0", Vec::new());
    let upgrader = Upgrader::new(config);

    match upgrader.upgrade(&downloaded, true) {
        Err(UpgradeError::Recovered(message)) => {
            assert!(
                message.contains("post-upgrade command 'sh -c echo boom; exit 3' failed: exit status 3"),
                "unexpected message: {message}"
            );
            assert!(message.contains("boom"), "output is included: {message}");
        }
        other => panic!("expected recovered error, got {other:?}"),
    }

    // Go installs the new index before the commands and keeps it.
    assert_eq!(upgrader.index().expect("index").version, "2.0.0");
}

#[test]
fn post_upgrade_command_runs_for_a_matching_trigger() {
    let tree = TempTree::new("command-trigger");
    std::fs::create_dir_all(tree.path("bin")).expect("create bin");
    prepare_download(&tree, &[("tool", b"new")]);

    let marker = tree.path("marker");
    let mut config = config(&tree);
    config.post_upgrade_commands = vec![
        UpdateCommandConfig {
            command: "sh".to_string(),
            args: vec![
                "-c".to_string(),
                format!("touch {}", marker.display()),
            ],
            trigger_artifact_fname: tree.path_str("bin/tool"),
            fail_on_error: true,
        },
        UpdateCommandConfig {
            command: "sh".to_string(),
            args: vec!["-c".to_string(), "touch /nonexistent-dir/x".to_string()],
            trigger_artifact_fname: tree.path_str("bin/other"),
            fail_on_error: true,
        },
    ];

    let downloaded = index_with("2.0.0", vec![artifact("tool", b"new")]);
    Upgrader::new(config)
        .upgrade(&downloaded, true)
        .expect("upgrade");

    assert!(Path::new(&marker).exists(), "triggered command ran");
}
