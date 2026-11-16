//! Tests for applying filter list updates (Go `filterlists/updater.go`).

use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_intel::{
    AppliedUpdate, EntryResource, FilterLists, ListEntry, BASE_LIST_FILE,
    INTERMEDIATE_LIST_FILE, URGENT_LIST_FILE,
};
use pm_updates::Artifact;

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

fn filter_lists() -> FilterLists {
    FilterLists::with_capacity(cache(), 1000, 100, 10, 100, 100)
}

fn entry(type_: &str, entity: &str, sources: &[&str]) -> ListEntry {
    ListEntry {
        type_: type_.to_string(),
        entity: entity.to_string(),
        whitelist: false,
        resources: sources
            .iter()
            .map(|source| EntryResource {
                source_id: (*source).to_string(),
                resource_id: "r1".to_string(),
            })
            .collect(),
    }
}

fn encode_uvarint(mut value: u64) -> Vec<u8> {
    let mut out = Vec::new();
    loop {
        let mut byte = (value & 0x7f) as u8;
        value >>= 7;
        if value > 0 {
            byte |= 0x80;
        }
        out.push(byte);
        if value == 0 {
            return out;
        }
    }
}

/// Writes a plain DSDL list file and returns its path.
fn write_list(dir: &std::path::Path, name: &str, entries: &[ListEntry]) -> String {
    let mut data = vec![pm_dsd::format::LIST, pm_dsd::format::JSON];
    for entry in entries {
        let json = serde_json::to_vec(entry).expect("entry json");
        data.extend(encode_uvarint(json.len() as u64));
        data.extend(json);
    }
    let path = dir.join(name);
    std::fs::write(&path, data).expect("write list file");
    path.to_string_lossy().into_owned()
}

fn artifact(filename: &str, local_file: &str) -> Artifact {
    Artifact {
        filename: filename.to_string(),
        local_file: local_file.to_string(),
        ..Artifact::default()
    }
}

fn temp_dir(tag: &str) -> std::path::PathBuf {
    let dir = std::env::temp_dir().join(format!(
        "pm-intel-apply-{tag}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    std::fs::create_dir_all(&dir).expect("create dir");
    dir
}

fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64
}

#[test]
fn applies_a_base_and_an_incremental_file() {
    let dir = temp_dir("base");
    let base = write_list(
        &dir,
        "base.dsdl",
        &[
            entry("domain", "Example.COM", &["list-a"]),
            entry("ipv4", "192.0.2.10", &["list-b"]),
            ListEntry {
                type_: "domain".to_string(),
                entity: "removed.example".to_string(),
                whitelist: true,
                resources: Vec::new(),
            },
        ],
    );
    let intermediate = write_list(
        &dir,
        "intermediate.dsdl",
        &[entry("domain", "extra.example", &["list-c"])],
    );

    let lists = filter_lists();
    let started_at = now();
    let report = lists
        .apply_update(
            &[
                artifact(BASE_LIST_FILE, &base),
                artifact(INTERMEDIATE_LIST_FILE, &intermediate),
            ],
            started_at,
        )
        .expect("apply update");

    assert_eq!(
        report,
        AppliedUpdate {
            files: 2,
            entries: 4,
            rebuilt: true
        }
    );
    assert!(lists.is_loaded());

    // Domains are normalized and found through bloom filters and records.
    assert_eq!(
        lists.lookup_domain("example.com.").expect("lookup"),
        vec!["list-a".to_string()]
    );
    assert_eq!(
        lists.lookup_domain("extra.example.").expect("lookup"),
        vec!["list-c".to_string()]
    );
    assert_eq!(
        lists.lookup_ip("192.0.2.10".parse().unwrap()).expect("lookup"),
        vec!["list-b".to_string()]
    );
    // A whitelist entry is stored as a deleted record, so it is not listed.
    assert!(lists
        .lookup_domain("removed.example.")
        .expect("lookup")
        .is_empty());

    let _ = std::fs::remove_dir_all(&dir);
}

#[test]
fn incremental_updates_keep_the_existing_bloom_filters() {
    let dir = temp_dir("incremental");
    let base = write_list(&dir, "base.dsdl", &[entry("domain", "base.example", &["a"])]);
    let urgent = write_list(&dir, "urgent.dsdl", &[entry("domain", "urgent.example", &["b"])]);
    let lists = filter_lists();

    lists
        .apply_update(&[artifact(BASE_LIST_FILE, &base)], now())
        .expect("base update");
    let report = lists
        .apply_update(&[artifact(URGENT_LIST_FILE, &urgent)], now())
        .expect("urgent update");
    assert!(!report.rebuilt);

    // Both entries are found: the bloom filters were extended, not rebuilt.
    assert_eq!(
        lists.lookup_domain("base.example.").expect("lookup"),
        vec!["a".to_string()]
    );
    assert_eq!(
        lists.lookup_domain("urgent.example.").expect("lookup"),
        vec!["b".to_string()]
    );

    let _ = std::fs::remove_dir_all(&dir);
}

#[test]
fn a_new_base_file_replaces_the_bloom_filters() {
    let dir = temp_dir("rebuild");
    let first = write_list(&dir, "base1.dsdl", &[entry("domain", "old.example", &["a"])]);
    let second = write_list(&dir, "base2.dsdl", &[entry("domain", "new.example", &["b"])]);
    let lists = filter_lists();

    lists
        .apply_update(&[artifact(BASE_LIST_FILE, &first)], now())
        .expect("first base");
    let report = lists
        .apply_update(&[artifact(BASE_LIST_FILE, &second)], now())
        .expect("second base");
    assert!(report.rebuilt);

    assert!(!lists.bloom_test("domain", "old.example."));
    assert!(lists.bloom_test("domain", "new.example."));
    assert!(lists
        .lookup_domain("old.example.")
        .expect("lookup")
        .is_empty());
    assert_eq!(
        lists.lookup_domain("new.example.").expect("lookup"),
        vec!["b".to_string()]
    );

    let _ = std::fs::remove_dir_all(&dir);
}

#[test]
fn base_update_removes_obsolete_entries() {
    let dir = temp_dir("cleanup");
    let base = write_list(
        &dir,
        "base.dsdl",
        &[entry("domain", "current.example", &["a"])],
    );
    let lists = filter_lists();

    // An entry from an old update, written an hour ago.
    lists
        .store_entry(&entry("domain", "stale.example", &["old"]), now() - 3600)
        .expect("store stale");

    lists
        .apply_update(&[artifact(BASE_LIST_FILE, &base)], now())
        .expect("base update");

    let records = lists
        .cache()
        .query(&pm_database::Query::new(pm_intel::LIST_KEY_PREFIX))
        .expect("query");
    let entities: Vec<String> = records
        .iter()
        .filter_map(|record| {
            serde_json::from_slice::<pm_intel::EntityRecord>(&record.data)
                .ok()
                .map(|entity| entity.value)
        })
        .collect();
    assert!(entities.contains(&"current.example.".to_string()), "{entities:?}");
    assert!(!entities.contains(&"stale.example.".to_string()), "{entities:?}");

    let _ = std::fs::remove_dir_all(&dir);
}
