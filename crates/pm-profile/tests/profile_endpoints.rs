//! Tests for adding endpoint rules to a profile (Go `Profile.AddEndpoint`).

use pm_profile::{Profile, CFG_OPTION_ENDPOINTS_KEY, CFG_OPTION_SERVICE_ENDPOINTS_KEY};
use serde_json::{json, Map, Value};

fn profile() -> Profile {
    Profile::new(Profile {
        source: "local".to_string(),
        id: "test".to_string(),
        ..Profile::default()
    })
}

fn endpoints(profile: &Profile, key: &str) -> Value {
    profile
        .config
        .get("filter")
        .and_then(|filter| filter.get(key.rsplit('/').next().unwrap()))
        .cloned()
        .unwrap_or(Value::Null)
}

#[test]
fn adds_the_first_rule() {
    let mut profile = profile();
    assert!(profile.add_endpoint("+ example.com"));

    assert_eq!(
        endpoints(&profile, CFG_OPTION_ENDPOINTS_KEY),
        json!(["+ example.com"])
    );
    assert_eq!(profile.config["filter"]["serviceEndpoints"], Value::Null);
}

#[test]
fn prepends_new_rules() {
    let mut profile = profile();
    assert!(profile.add_endpoint("+ example.com"));
    assert!(profile.add_endpoint("+ other.com"));

    assert_eq!(
        endpoints(&profile, CFG_OPTION_ENDPOINTS_KEY),
        json!(["+ other.com", "+ example.com"])
    );
}

#[test]
fn ignores_identical_rules_in_the_same_prefix_block() {
    let mut profile = profile();
    assert!(profile.add_endpoint("+ example.com"));
    assert!(!profile.add_endpoint("+ example.com"));
    assert_eq!(
        endpoints(&profile, CFG_OPTION_ENDPOINTS_KEY),
        json!(["+ example.com"])
    );
}

#[test]
fn stops_comparing_at_the_first_other_prefix_like_go() {
    let mut profile = profile();
    assert!(profile.add_endpoint("+ example.com"));
    assert!(profile.add_endpoint("- example.com"));

    // The list is now ["- example.com", "+ example.com"]. Adding the deny rule
    // again looks at "- example.com" first and finds the duplicate.
    assert!(!profile.add_endpoint("- example.com"));
    assert_eq!(
        endpoints(&profile, CFG_OPTION_ENDPOINTS_KEY),
        json!(["- example.com", "+ example.com"])
    );

    // Adding the permit rule again compares only against the first (deny)
    // entry and stops there, so Go adds a duplicate instead of finding it.
    assert!(profile.add_endpoint("+ example.com"));
    assert_eq!(
        endpoints(&profile, CFG_OPTION_ENDPOINTS_KEY),
        json!(["+ example.com", "- example.com", "+ example.com"])
    );
}

#[test]
fn service_rules_use_the_service_key() {
    let mut profile = profile();
    assert!(profile.add_service_endpoint("+ 1.2.3.4"));
    assert_eq!(
        endpoints(&profile, CFG_OPTION_SERVICE_ENDPOINTS_KEY),
        json!(["+ 1.2.3.4"])
    );
    assert_eq!(profile.config["filter"]["endpoints"], Value::Null);
}

#[test]
fn reads_rules_from_an_expanded_flat_config() {
    let mut profile = Profile::new(Profile {
        source: "local".to_string(),
        id: "test".to_string(),
        config: Map::from_iter([(
            "filter/endpoints".to_string(),
            json!(["* example.com LAN"]),
        )]),
        ..Profile::default()
    });

    assert!(profile.add_endpoint("+ example.com"));
    assert_eq!(
        endpoints(&profile, CFG_OPTION_ENDPOINTS_KEY),
        json!(["+ example.com", "* example.com LAN"])
    );
}
