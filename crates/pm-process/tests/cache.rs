//! Tests of the process cache, tags and the process tree database.

mod common;

use std::collections::HashSet;
use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_process::{
    Process, ProcessCache, SpecialProcess, Tag, TagDescription, TagHandler, UNIDENTIFIED_PROCESS_ID,
};

struct MountHandler;

impl TagHandler for MountHandler {
    fn name(&self) -> &str {
        "mounts"
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        vec![TagDescription {
            id: "app-image-path".to_string(),
            name: "AppImage".to_string(),
            description: "Path of the mounted AppImage.".to_string(),
        }]
    }

    fn add_tags(&self, process: &mut Process) {
        if process.path.starts_with("/tmp/.mount_") {
            process.tags.push(Tag {
                key: "app-image-path".to_string(),
                value: process.path.clone(),
            });
            process.matching_path = process.path.clone();
        }
    }
}

fn setup(tag: &str) -> (std::path::PathBuf, Arc<ProcessCache>) {
    let root = common::temp_dir(tag);
    common::set_boot_time(&root, 1_700_000_000);
    common::add_process(&root, 1000, 1, 1000, 100, "leader", "/usr/bin/leader");
    common::add_process(&root, 1500, 1000, 1000, 200, "parent", "/usr/bin/parent");
    common::add_process(&root, 2000, 1500, 1000, 300, "child", "/usr/bin/child");
    let passwd = common::write_passwd(&root);
    let cache = common::cache(&root, &passwd);
    (root, cache)
}

#[test]
fn loads_and_caches_processes() {
    let (root, cache) = setup("cache");

    let process = cache.get_or_find(2000).expect("process 2000");
    assert_eq!(process.pid, 2000);
    assert_eq!(process.name, "child");
    assert_eq!(process.user_id, 1000);
    assert_eq!(process.user_name, "testuser");
    assert_eq!(process.parent_pid, 1500);
    assert_eq!(process.leader_pid, 1000);
    assert_eq!(process.exec_name, "child");
    assert_eq!(process.path, "/usr/bin/child");
    assert_eq!(process.cmd_line, "myproc --flag");
    assert_eq!(process.env["HOME"], "/home/test");
    assert_eq!(process.created_at, 1_700_000_003_000);
    assert!(process.first_seen > 0);
    assert!(process.is_identified());
    assert!(process.has_valid_pid());
    assert_eq!(process.key(), "2000-1700000003000");

    // The parent creation time is resolved from the parent process.
    assert_eq!(process.parent_created_at, 1_700_000_002_000);

    // The same process is only loaded once.
    let again = cache.get_or_find(2000).unwrap();
    assert!(Arc::ptr_eq(&process, &again));
    assert_eq!(cache.all().len(), 1);

    // A restarted process with the same PID gets a new key.
    common::add_process(&root, 2000, 1500, 1000, 400, "child", "/usr/bin/child");
    let restarted = cache.get_or_find(2000).unwrap();
    assert_eq!(restarted.key(), "2000-1700000004000");
    assert_ne!(restarted.key(), process.key());
    // `all()` is keyed by PID, like in Go; both entries stay in the storage.
    assert_eq!(cache.all().len(), 1);
    assert!(cache.get_from_storage(process.key()).is_some());
    assert!(cache.get_from_storage(restarted.key()).is_some());

    // Missing processes are reported.
    assert!(cache.get_or_find(9999).is_err());
}

#[test]
fn resolves_process_group_leaders() {
    let (_root, cache) = setup("leader");

    let (process, leader) = cache.get_with_leader(2000).expect("process");
    assert_eq!(process.pid, 2000);
    assert_eq!(leader.expect("leader").pid, 1000);

    // The leader itself has no leader.
    let leader = cache.get_or_find(1000).unwrap();
    assert!(cache.leader(&leader).is_none());
}

#[test]
fn special_processes_are_stable() {
    let (_root, cache) = setup("special");

    let unidentified = cache.get_or_find(UNIDENTIFIED_PROCESS_ID).unwrap();
    assert_eq!(unidentified.pid, UNIDENTIFIED_PROCESS_ID);
    assert_eq!(unidentified.name, "Other Connections");
    assert!(!unidentified.is_identified());
    assert_eq!(unidentified.key(), "-2-0");
    assert!(Arc::ptr_eq(
        &unidentified,
        &cache.special(SpecialProcess::Unidentified)
    ));

    let unsolicited = cache.get_or_find(-3).unwrap();
    assert_eq!(unsolicited.name, "Network Noise");

    let system = cache.get_or_find(0).unwrap();
    assert_eq!(system.name, "Operating System");
    assert_eq!(system.user_name, "Kernel");
}

#[test]
fn tag_handlers_are_applied() {
    let root = common::temp_dir("tags");
    common::set_boot_time(&root, 1_700_000_000);
    common::add_process(
        &root,
        3000,
        1,
        3000,
        100,
        "appimage",
        "/tmp/.mount_App/AppRun",
    );
    let passwd = common::write_passwd(&root);
    let cache = common::cache(&root, &passwd);

    // Duplicate names are rejected.
    cache.register_tag_handler(Arc::new(MountHandler)).unwrap();
    assert!(cache.register_tag_handler(Arc::new(MountHandler)).is_err());
    assert_eq!(cache.tag_descriptions().len(), 1);

    let process = cache.get_or_find(3000).unwrap();
    let tag = process.tag("app-image-path").expect("app image tag");
    assert_eq!(tag.value, "/tmp/.mount_App/AppRun");
    assert_eq!(process.matching_path, "/tmp/.mount_App/AppRun");
}

#[test]
fn clean_storage_removes_stale_processes() {
    let (root, cache) = setup("clean");

    let process = cache.get_or_find(2000).unwrap();
    let key = process.key().to_string();
    let last_seen = pm_process::now() - 3600;

    // Remove the process from the fake proc tree and age it.
    std::fs::remove_dir_all(root.join("2000")).unwrap();
    cache.set_last_seen(&key, last_seen).unwrap();

    // Other processes are still active.
    let mut active = HashSet::new();
    cache.clean_storage(&mut active);
    assert!(active.contains(&1500));
    assert!(
        cache.get_from_storage(&key).is_none(),
        "stale process deleted"
    );

    // A process that was never seen long enough gets a timestamp first.
    let parent = cache.get_or_find(1500).unwrap();
    std::fs::remove_dir_all(root.join("1500")).unwrap();
    cache.clean_storage(&mut HashSet::new());
    let parent = cache
        .get_from_storage(parent.key())
        .expect("parent is kept for now");
    assert!(parent.last_seen > 0);
}

#[test]
fn database_updates_are_pushed() {
    let (_root, cache) = setup("database");

    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("network", "network database")
        .unwrap();
    let controller = databases
        .inject("network", Box::new(MemoryStorage::new()))
        .unwrap();
    cache.set_db_controller(controller);

    let internal = databases.internal_interface();
    let mut subscription = internal
        .subscribe(pm_database::Query::new("network:"))
        .unwrap();

    let process = cache.get_or_find(2000).unwrap();
    let record = subscription.try_recv().expect("process update");
    assert_eq!(
        record.key.to_string(),
        format!("network:tree/{}", process.key())
    );
    let json: serde_json::Value = serde_json::from_slice(&record.data).unwrap();
    assert_eq!(json["Pid"], 2000);
    assert_eq!(json["Name"], "child");
    assert_eq!(json["CreatedAt"], 1_700_000_003_000i64);
    assert_eq!(json["ParentPid"], 1500);
    assert_eq!(json["LeaderPid"], 1000);
    assert_eq!(json["Tags"], serde_json::json!([]));

    cache.delete(&process);
    let record = subscription.try_recv().expect("delete update");
    assert!(record.meta.deleted > 0);
    assert!(record.data.is_empty());
    assert!(cache.get_from_storage(process.key()).is_none());
}

#[test]
fn configuration_option_is_registered() {
    let config = pm_config::Config::new();
    pm_process::register_options(&config).unwrap();

    let enabled = pm_process::enabled_getter(&config);
    assert!(enabled());

    config
        .set_config_option(
            pm_process::CFG_OPTION_ENABLE_PROCESS_DETECTION_KEY,
            serde_json::json!(false),
        )
        .unwrap();
    assert!(!enabled());
    assert_eq!(
        config
            .option(pm_process::CFG_OPTION_ENABLE_PROCESS_DETECTION_KEY)
            .unwrap()
            .definition()
            .name,
        "Process Detection"
    );
}

#[test]
fn finds_matching_profiles() {
    use pm_profile::{Fingerprint, Profile, SOURCE_LOCAL};

    let (root, cache) = setup("match");
    let process = cache.get_or_find(2000).unwrap();

    let exact = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "exact".to_string(),
        fingerprints: vec![Fingerprint::new("path", "", "equals", "/usr/bin/child")],
        ..Profile::default()
    });
    let prefix = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "prefix".to_string(),
        fingerprints: vec![Fingerprint::new("path", "", "prefix", "/usr/bin/")],
        ..Profile::default()
    });
    let other = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "other".to_string(),
        fingerprints: vec![Fingerprint::new("path", "", "equals", "/opt/other")],
        ..Profile::default()
    });

    let profiles = vec![prefix, other, exact];
    let matched = process.find_profile(&profiles).expect("a profile");
    assert_eq!(matched.profile.name, "exact");
    assert_eq!(process.matching_data().path(), "/usr/bin/child");

    assert!(process.find_profile(&[]).is_none());
    let _ = root;
}
