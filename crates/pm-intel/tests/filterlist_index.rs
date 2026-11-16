//! Tests for the filter list index (Go `filterlists/index.go`).

use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_intel::{
    are_semvers_equal, resolve_list_ids, unbreak_filter_list_ids, update_list_index, Category,
    ListIndexFile, Source, INDEX_KEY,
};

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

fn index() -> ListIndexFile {
    ListIndexFile {
        version: "v2025.04.14".to_string(),
        schema_version: "1".to_string(),
        categories: vec![
            Category {
                id: "ADS".to_string(),
                name: "Ads".to_string(),
                ..Category::default()
            },
            Category {
                id: "TRACKERS".to_string(),
                parent: "ADS".to_string(),
                name: "Trackers".to_string(),
                ..Category::default()
            },
            Category {
                id: "UNBREAK".to_string(),
                name: "Unbreak".to_string(),
                ..Category::default()
            },
        ],
        sources: vec![
            Source {
                id: "ads-1".to_string(),
                category: "ADS".to_string(),
                ..Source::default()
            },
            Source {
                id: "tracker-1".to_string(),
                category: "TRACKERS".to_string(),
                ..Source::default()
            },
            Source {
                id: "unbreak-1".to_string(),
                category: "UNBREAK".to_string(),
                ..Source::default()
            },
        ],
    }
}

#[test]
fn resolves_categories_and_sources() {
    let index = index();

    assert_eq!(
        index.distinct_source_ids(&["ADS".to_string()]),
        vec!["ads-1".to_string(), "tracker-1".to_string()]
    );
    assert_eq!(
        index.distinct_source_ids(&["TRACKERS".to_string()]),
        vec!["tracker-1".to_string()]
    );
    assert_eq!(
        index.distinct_source_ids(&["ads-1".to_string()]),
        vec!["ads-1".to_string()]
    );
    assert_eq!(
        index.distinct_source_ids(&["ADS".to_string(), "ads-1".to_string()]),
        vec!["ads-1".to_string(), "tracker-1".to_string()]
    );
    assert!(index.distinct_source_ids(&["UNKNOWN".to_string()]).is_empty());
}

#[test]
fn cyclic_categories_do_not_loop_forever() {
    let mut index = index();
    index.categories.push(Category {
        id: "CYCLE".to_string(),
        parent: "CYCLE".to_string(),
        name: "Cycle".to_string(),
        ..Category::default()
    });
    assert!(index.distinct_source_ids(&["CYCLE".to_string()]).is_empty());
}

#[test]
fn semver_equality_matches_go() {
    assert!(are_semvers_equal("2025.04.14", "2025.4.14"));
    assert!(are_semvers_equal("v1.2.3", "1.2.3"));
    assert!(are_semvers_equal("1.2", "1.2.0"));
    assert!(!are_semvers_equal("1.2.3", "1.2.4"));
    assert!(!are_semvers_equal("not-a-version", "not-a-version"));
}

#[test]
fn stores_and_loads_the_index() {
    let cache = cache();
    let index = index();

    assert!(ListIndexFile::load(&cache).expect("load").is_none());
    index.store(&cache).expect("store");

    let loaded = ListIndexFile::load(&cache).expect("load").expect("index");
    assert_eq!(loaded, index);
    assert!(cache.get(INDEX_KEY).is_ok());
}

#[test]
fn resolves_ids_through_the_cache() {
    let cache = cache();
    assert_eq!(
        resolve_list_ids(&cache, &["ADS".to_string()]).expect("resolve"),
        None
    );

    index().store(&cache).expect("store");
    assert_eq!(
        resolve_list_ids(&cache, &["ADS".to_string()])
            .expect("resolve")
            .expect("index"),
        vec!["ads-1".to_string(), "tracker-1".to_string()]
    );
    assert_eq!(
        unbreak_filter_list_ids(&cache)
            .expect("unbreak")
            .expect("index"),
        vec!["unbreak-1".to_string()]
    );
}

#[test]
fn parses_a_json_index() {
    let data =
        pm_dsd::dump(&index(), pm_dsd::format::JSON).expect("dump dsd");
    let parsed = ListIndexFile::from_bytes(&data).expect("parse");
    assert_eq!(parsed.version, "v2025.04.14");
    assert_eq!(parsed.categories.len(), 3);
}

#[test]
fn updates_the_cache_from_a_file() {
    let dir = std::env::temp_dir().join(format!(
        "pm-intel-index-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    std::fs::create_dir_all(&dir).expect("create dir");
    let file = dir.join("index.dsd");
    std::fs::write(
        &file,
        pm_dsd::dump(&index(), pm_dsd::format::JSON).unwrap(),
    )
    .expect("write index");

    let cache = cache();
    assert!(update_list_index(&cache, &file, "2025.04.14").expect("update"));
    assert!(!update_list_index(&cache, &file, "2025.04.14").expect("unchanged"));
    assert!(!update_list_index(&cache, &file, "2025.4.14").expect("semver equal"));
    assert!(update_list_index(&cache, &file, "2025.05.01").expect("new version"));

    let _ = std::fs::remove_dir_all(&dir);
}
