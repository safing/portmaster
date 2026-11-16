//! Tests for the filter list update planning (Go `filterlists/updater.go`).

use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_intel::{
    cache_database_version_or_zero, get_cache_database_version, highest_version, is_base_file,
    resolve_update_order, set_cache_database_version, without_valid_version, BASE_LIST_FILE,
    INTERMEDIATE_LIST_FILE, URGENT_LIST_FILE,
};
use pm_updates::{Artifact, Version};

fn cache() -> pm_database::Interface {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .expect("register");
    databases
        .inject("cache", Box::new(MemoryStorage::new()))
        .expect("inject");
    pm_intel::cache_interface(&databases)
}

fn artifact(filename: &str, version: &str) -> Artifact {
    Artifact {
        filename: filename.to_string(),
        version: version.to_string(),
        version_num: Version::parse(version).ok(),
        ..Artifact::default()
    }
}

fn names(artifacts: &[Artifact]) -> Vec<&str> {
    artifacts
        .iter()
        .map(|artifact| artifact.filename.as_str())
        .collect()
}

#[test]
fn base_file_detection() {
    assert!(is_base_file(BASE_LIST_FILE));
    assert!(!is_base_file(INTERMEDIATE_LIST_FILE));
    assert!(!is_base_file(URGENT_LIST_FILE));
}

#[test]
fn cache_version_roundtrip() {
    let cache = cache();
    assert_eq!(get_cache_database_version(&cache).expect("get"), None);
    assert_eq!(cache_database_version_or_zero(&cache).to_string(), "0.0.0");

    set_cache_database_version(&cache, "v2025.04.14").expect("set");
    let version = get_cache_database_version(&cache)
        .expect("get")
        .expect("version");
    assert_eq!(version, Version::parse("2025.4.14").unwrap());
    assert_eq!(
        cache_database_version_or_zero(&cache),
        Version::parse("2025.4.14").unwrap()
    );
}

#[test]
fn update_order_starts_at_a_newer_base_file() {
    let mut files = vec![
        artifact(URGENT_LIST_FILE, "v2.5.0"),
        artifact(INTERMEDIATE_LIST_FILE, "v1.0.0"),
        artifact(BASE_LIST_FILE, "v2.0.0"),
    ];
    let (mut valid, invalid) = without_valid_version(std::mem::take(&mut files));
    assert!(invalid.is_empty());

    let order = resolve_update_order(std::mem::take(&mut valid), &Version::parse("v1.7.0").unwrap());
    // Sorted ascending, the base file at 2.0 restarts the update.
    assert_eq!(names(&order), vec![BASE_LIST_FILE, URGENT_LIST_FILE]);
    assert_eq!(highest_version(&order), Some("v2.5.0"));

    // A cache at 2.2 only needs the urgent file.
    let order = resolve_update_order(
        vec![
            artifact(URGENT_LIST_FILE, "v2.5.0"),
            artifact(BASE_LIST_FILE, "v2.0.0"),
            artifact(INTERMEDIATE_LIST_FILE, "v2.1.0"),
        ],
        &Version::parse("v2.2.0").unwrap(),
    );
    assert_eq!(names(&order), vec![URGENT_LIST_FILE]);
}

#[test]
fn nothing_to_do_when_cache_is_current() {
    let order = resolve_update_order(
        vec![
            artifact(BASE_LIST_FILE, "v2.0.0"),
            artifact(URGENT_LIST_FILE, "v2.5.0"),
        ],
        &Version::parse("v3.0.0").unwrap(),
    );
    assert!(order.is_empty());
    assert_eq!(highest_version(&order), None);
}

#[test]
fn artifacts_without_a_version_are_skipped() {
    let files = vec![
        artifact(BASE_LIST_FILE, ""),
        artifact(URGENT_LIST_FILE, "v1.2.3"),
    ];
    let (valid, invalid) = without_valid_version(files);
    assert_eq!(names(&valid), vec![URGENT_LIST_FILE]);
    assert_eq!(names(&invalid), vec![BASE_LIST_FILE]);

    let order = resolve_update_order(valid, &Version::parse("v0.0.0").unwrap());
    assert_eq!(names(&order), vec![URGENT_LIST_FILE]);
}
