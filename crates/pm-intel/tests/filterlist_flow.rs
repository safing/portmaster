//! Tests for the filter list update flow (Go `filterlists/performUpdate`).

use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_intel::{
    update_filter_lists, Category, EntryResource, FilterLists, ListEntry, ListIndexFile, Source,
    BASE_LIST_FILE, URGENT_LIST_FILE,
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

fn entry(entity: &str, source: &str) -> ListEntry {
    ListEntry {
        type_: "domain".to_string(),
        entity: entity.to_string(),
        whitelist: false,
        resources: vec![EntryResource {
            source_id: source.to_string(),
            resource_id: "r1".to_string(),
        }],
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

fn artifact(filename: &str, local_file: &str, version: &str) -> Artifact {
    Artifact {
        filename: filename.to_string(),
        local_file: local_file.to_string(),
        version: version.to_string(),
        version_num: Version::parse(version).ok(),
        ..Artifact::default()
    }
}

fn temp_dir() -> std::path::PathBuf {
    let dir = std::env::temp_dir().join(format!(
        "pm-intel-flow-{}-{}",
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
fn applies_pending_files_and_tracks_the_cache_version() {
    let dir = temp_dir();
    let base = write_list(&dir, "base.dsdl", &[entry("base.example", "a")]);
    let urgent = write_list(&dir, "urgent.dsdl", &[entry("urgent.example", "b")]);
    let index_file = dir.join("index.dsd");
    let index = ListIndexFile {
        version: "v2025.04.14".to_string(),
        categories: vec![Category {
            id: "ADS".to_string(),
            name: "Ads".to_string(),
            ..Category::default()
        }],
        sources: vec![Source {
            id: "a".to_string(),
            category: "ADS".to_string(),
            ..Source::default()
        }],
        ..ListIndexFile::default()
    };
    std::fs::write(
        &index_file,
        pm_dsd::dump(&index, pm_dsd::format::JSON).expect("index dsd"),
    )
    .expect("write index");

    let lists = FilterLists::with_capacity(cache(), 1000, 100, 10, 100, 100);
    let cache_db = lists.cache();

    // First run: both files are newer than the empty cache and are applied.
    let update = update_filter_lists(
        &lists,
        cache_db,
        &index_file,
        "2025.04.14",
        vec![
            artifact(BASE_LIST_FILE, &base, "2.0.0"),
            artifact(URGENT_LIST_FILE, &urgent, "2.5.0"),
        ],
        now(),
    )
    .expect("update")
    .expect("pending update");
    assert_eq!(update.version, "2.5.0");
    assert!(update.report.rebuilt);
    assert_eq!(update.report.files, 2);
    assert_eq!(
        lists.lookup_domain("base.example.").expect("lookup"),
        vec!["a".to_string()]
    );
    assert_eq!(
        lists.lookup_domain("urgent.example.").expect("lookup"),
        vec!["b".to_string()]
    );

    // The index was cached as a side effect.
    assert!(ListIndexFile::load(cache_db).expect("load").is_some());

    // Second run: the cache is current.
    assert_eq!(
        update_filter_lists(
            &lists,
            cache_db,
            &index_file,
            "2025.04.14",
            vec![
                artifact(BASE_LIST_FILE, &base, "2.0.0"),
                artifact(URGENT_LIST_FILE, &urgent, "2.5.0"),
            ],
            now(),
        )
        .expect("update"),
        None
    );

    // A newer urgent file is applied on top without rebuilding.
    let newest = write_list(&dir, "urgent2.dsdl", &[entry("newest.example", "c")]);
    let update = update_filter_lists(
        &lists,
        cache_db,
        &index_file,
        "2025.04.14",
        vec![
            artifact(BASE_LIST_FILE, &base, "2.0.0"),
            artifact(URGENT_LIST_FILE, &urgent, "2.5.0"),
            artifact(URGENT_LIST_FILE, &newest, "3.0.0"),
        ],
        now(),
    )
    .expect("update")
    .expect("newest update");
    assert_eq!(update.version, "3.0.0");
    assert!(!update.report.rebuilt);
    assert_eq!(update.report.files, 1);
    assert_eq!(
        lists.lookup_domain("newest.example.").expect("lookup"),
        vec!["c".to_string()]
    );
    assert_eq!(
        lists.lookup_domain("base.example.").expect("lookup"),
        vec!["a".to_string()]
    );

    let _ = std::fs::remove_dir_all(&dir);
}
