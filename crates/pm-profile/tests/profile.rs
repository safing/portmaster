//! Behavior tests of profiles, matching, merging and the profile store.

use std::collections::BTreeMap;
use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_profile::{
    find_matching_profile, merge_profiles, Fingerprint, MatchingData, Profile, ProfileStore, Tag,
    SOURCE_LOCAL,
};
use serde_json::json;

struct TestData {
    tags: Vec<Tag>,
    env: BTreeMap<String, String>,
    path: String,
    matching_path: String,
    cmdline: String,
}

impl TestData {
    fn with_path(path: &str) -> TestData {
        TestData {
            tags: Vec::new(),
            env: BTreeMap::new(),
            path: path.to_string(),
            matching_path: String::new(),
            cmdline: String::new(),
        }
    }
}

impl MatchingData for TestData {
    fn tags(&self) -> &[Tag] {
        &self.tags
    }

    fn env(&self) -> &BTreeMap<String, String> {
        &self.env
    }

    fn path(&self) -> &str {
        &self.path
    }

    fn matching_path(&self) -> &str {
        &self.matching_path
    }

    fn cmdline(&self) -> &str {
        &self.cmdline
    }
}

fn profile_with_fingerprint(value: &str) -> Profile {
    Profile {
        source: SOURCE_LOCAL.to_string(),
        name: value.to_string(),
        fingerprints: vec![Fingerprint::new("path", "", "equals", value)],
        ..Profile::default()
    }
}

#[test]
fn new_derives_ids_and_expands_config() {
    let profile = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "Test".to_string(),
        fingerprints: vec![Fingerprint::new("path", "", "equals", "/usr/bin/test")],
        config: json!({
            "filter/defaultAction": "block",
            "filter/endpoints": ["+ 1.1.1.1"],
        })
        .as_object()
        .cloned()
        .unwrap(),
        ..Profile::default()
    });

    assert!(!profile.id.is_empty());
    assert_eq!(profile.scoped_id(), format!("local/{}", profile.id));
    assert_eq!(profile.key(), format!("core:profiles/local/{}", profile.id));
    assert!(profile.created > 0);
    assert!(profile.saved_internally);

    // The flat config was expanded into a nested object.
    assert_eq!(profile.config["filter"]["endpoints"][0], "+ 1.1.1.1");
    assert_eq!(
        profile.config["filter"]["defaultAction"].as_str(),
        Some("block")
    );

    // Profiles without fingerprints get a random ID.
    let random = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        ..Profile::default()
    });
    assert_eq!(random.id.len(), 36);
    assert!(random.id.contains('-'));

    // The deprecated LinkedPath becomes the PresentationPath.
    let migrated = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        linked_path: "/usr/bin/legacy".to_string(),
        ..Profile::default()
    });
    assert_eq!(migrated.presentation_path, "/usr/bin/legacy");
}

#[test]
fn matching_finds_the_best_profile() {
    let exact = Profile::new(profile_with_fingerprint("/usr/bin/curl"));
    let prefix = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "prefix".to_string(),
        fingerprints: vec![Fingerprint::new("path", "", "prefix", "/usr/bin/")],
        ..Profile::default()
    });
    let other = Profile::new(profile_with_fingerprint("/usr/bin/other"));
    let profiles = vec![prefix, other, exact];

    let data = TestData::with_path("/usr/bin/curl");
    let matched = find_matching_profile(&profiles, &data).expect("a match");
    assert_eq!(matched.profile.name, "/usr/bin/curl");
    // equals (3000 + 14) beats prefix (2000 + 9).
    assert_eq!(matched.score, 10_000 + 3_000 + 13);
    assert!(matched.conflicts.is_empty());

    // No match.
    assert!(find_matching_profile(&profiles, &TestData::with_path("/opt/app")).is_none());

    // Ties are reported as conflicts; the first profile wins.
    let first = Profile::new(profile_with_fingerprint("/usr/bin/tie"));
    let second = Profile::new(profile_with_fingerprint("/usr/bin/tie"));
    let profiles = vec![first, second];
    let matched =
        find_matching_profile(&profiles, &TestData::with_path("/usr/bin/tie")).expect("a match");
    assert_eq!(matched.conflicts.len(), 2);
}

#[test]
fn merging_profiles_keeps_fingerprints_and_icons() {
    let primary = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "primary".to_string(),
        description: "primary description".to_string(),
        homepage: "https://example.com".to_string(),
        icons: vec![pm_profile::Icon::new("path", "/tmp/a.png", "user")],
        fingerprints: vec![Fingerprint::new("path", "", "equals", "/usr/bin/a")],
        config: json!({"filter": {"defaultAction": "block"}})
            .as_object()
            .cloned()
            .unwrap(),
        last_edited: 100,
        ..Profile::default()
    });
    let secondary = Arc::new(Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "secondary".to_string(),
        icons: vec![
            pm_profile::Icon::new("api", "/api/v1/icons/1", "core"),
            pm_profile::Icon::new("path", "/tmp/a.png", "user"),
        ],
        fingerprints: vec![Fingerprint::new("path", "", "equals", "/usr/bin/b")],
        ..Profile::default()
    }));

    let merged = merge_profiles("Merged", &primary, &[Arc::clone(&secondary)]).unwrap();
    assert_eq!(merged.name, "Merged");
    assert_eq!(merged.description, "primary description");
    assert_eq!(merged.homepage, "https://example.com");
    assert!(!merged.id.is_empty());
    assert!(merged.last_edited > 0);
    assert_eq!(merged.config["filter"]["defaultAction"], "block");

    // Icons are sorted and de-duplicated.
    assert_eq!(merged.icons.len(), 2);
    assert_eq!(merged.icons[0].type_, "api");

    // Fingerprints carry their origin and are de-duplicated.
    assert_eq!(merged.fingerprints.len(), 2);
    assert_eq!(merged.fingerprints[0].merged_from, primary.scoped_id());
    assert_eq!(merged.fingerprints[1].merged_from, secondary.scoped_id());

    // A merge needs at least one secondary profile.
    assert!(merge_profiles("Merged", &primary, &[]).is_err());
}

#[test]
fn new_compacts_duplicate_fingerprints_like_go() {
    // Go derives the ID from the sorted slice and compacts it in place, which
    // leaves a zeroed fingerprint at the end. This is kept for compatibility.
    let profile = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        fingerprints: vec![
            Fingerprint::new("path", "", "equals", "/usr/bin/test"),
            Fingerprint::new("path", "", "equals", "/usr/bin/test"),
        ],
        ..Profile::default()
    });
    assert_eq!(profile.fingerprints.len(), 2);
    assert_eq!(profile.fingerprints[0].value, "/usr/bin/test");
    assert!(profile.fingerprints[1].type_.is_empty());
}

#[test]
fn profile_store_reads_and_writes() {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("core", "core database")
        .unwrap();
    databases
        .inject("core", Box::new(MemoryStorage::new()))
        .unwrap();
    let store = ProfileStore::new(databases.internal_interface());

    let profile = Profile::new(profile_with_fingerprint("/usr/bin/curl"));
    store.save(&profile).unwrap();

    let loaded = store.get(&profile.scoped_id()).unwrap();
    assert_eq!(loaded.id, profile.id);
    assert_eq!(loaded.name, profile.name);
    assert_eq!(loaded.fingerprints, profile.fingerprints);
    assert_eq!(loaded.created, profile.created);

    let all = store.all(SOURCE_LOCAL).unwrap();
    assert_eq!(all.len(), 1);

    // Deleted profiles are gone.
    store.delete(&profile).unwrap();
    assert!(store.get(&profile.scoped_id()).is_err());

    // Invalid profiles are rejected.
    assert!(store.save(&Profile::default()).is_err());
}
