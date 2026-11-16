//! Tests for the Go-compatible entity JSON (used by prompt notifications).

mod common;

use pm_network::entity_to_json;
use serde_json::json;

#[test]
fn entity_json_matches_go_fields() {
    let mut entity = (common::new_entity_factory())();
    entity.ip = Some("1.1.1.1".parse().unwrap());
    entity.protocol = 6;
    entity.port = 443;
    entity.domain = "example.com".to_string();
    entity.init(443);

    let value = entity_to_json(&entity);
    assert_eq!(value["IP"], "1.1.1.1");
    assert_eq!(value["Protocol"], 6);
    assert_eq!(value["Port"], 443);
    assert_eq!(value["Domain"], "example.com");
    // Unset slices stay `null`, like Go nil slices.
    assert_eq!(value["CNAME"], serde_json::Value::Null);
    assert_eq!(value["BlockedByLists"], serde_json::Value::Null);
    // Location data is present as zero values.
    assert_eq!(value["Country"], "");
    assert_eq!(value["ASN"], 0);
    assert_eq!(value.as_object().expect("object").len(), 16);
}

#[test]
fn matched_lists_serialize_as_empty_arrays() {
    let mut entity = (common::new_entity_factory())();
    entity.ip = Some("1.1.1.1".parse().unwrap());
    entity.lists_matched = true;

    let value = entity_to_json(&entity);
    assert_eq!(value["BlockedByLists"], json!([]));
    assert_eq!(value["BlockedEntities"], json!([]));
}
