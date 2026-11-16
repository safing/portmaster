//! Byte-for-byte comparison with fixtures produced by the Go implementation.

use pm_sync::{
    serialize_export, serialize_profile_export, ProfileExport, ProfileFingerprint,
    SettingsExport, SingleSettingExport,
};
use serde_json::json;

const FIXTURES: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/tests/fixtures");

fn fixture(name: &str) -> Vec<u8> {
    std::fs::read(format!("{FIXTURES}/{name}")).expect("read fixture")
}

fn settings_export() -> SettingsExport {
    SettingsExport {
        type_: "settings".to_string(),
        config: json!({
            "filter": {
                "defaultAction": "block",
                "endpoints": ["+ example.com", "- bad.com"],
            },
            "core": { "devMode": true },
        })
        .as_object()
        .cloned()
        .unwrap(),
    }
}

fn single_export() -> SingleSettingExport {
    SingleSettingExport {
        type_: "single-setting".to_string(),
        id: "filter/defaultAction".to_string(),
        value: json!("block"),
    }
}

fn profile_export() -> ProfileExport {
    ProfileExport {
        type_: "profile".to_string(),
        id: "abc123".to_string(),
        source: "local".to_string(),
        name: "Test App".to_string(),
        description: "A test app".to_string(),
        homepage: "https://example.com".to_string(),
        presentation_path: "/usr/bin/test".to_string(),
        use_presentation_path: true,
        icon_data: "data:image/png;base64,iVBORw0KGgo=".to_string(),
        fingerprints: vec![
            ProfileFingerprint {
                type_: "path".to_string(),
                operation: "equals".to_string(),
                value: "/usr/bin/test".to_string(),
                ..Default::default()
            },
            ProfileFingerprint {
                type_: "tag".to_string(),
                key: "name".to_string(),
                operation: "equals".to_string(),
                value: "Test App".to_string(),
                merged_from: "local/other".to_string(),
            },
        ],
        config: json!({"filter": {"defaultAction": "block"}})
            .as_object()
            .cloned()
            .unwrap(),
        last_edited: Some("2023-11-14T22:30:00Z".to_string()),
        created: Some("2023-11-14T22:13:20Z".to_string()),
        internal: false,
    }
}

#[test]
fn settings_json_matches_go() {
    let (data, mime) = serialize_export(&settings_export(), "application/json").unwrap();
    assert_eq!(mime, "application/json");
    assert_eq!(data, fixture("settings.json"));
}

#[test]
fn settings_yaml_matches_go() {
    let (data, mime) = serialize_export(&settings_export(), "application/yaml").unwrap();
    assert_eq!(mime, "application/yaml");
    assert_eq!(data, fixture("settings.yaml"));
}

#[test]
fn single_setting_json_matches_go() {
    let (data, _) = serialize_export(&single_export(), "application/json").unwrap();
    assert_eq!(data, fixture("single_setting.json"));
}

#[test]
fn single_setting_yaml_matches_go() {
    let (data, _) = serialize_export(&single_export(), "application/yaml").unwrap();
    assert_eq!(data, fixture("single_setting.yaml"));
}

#[test]
fn profile_json_matches_go() {
    let (data, _) = serialize_profile_export(&profile_export(), "application/json").unwrap();
    assert_eq!(data, fixture("profile.json"));
}

#[test]
fn profile_yaml_with_icon_matches_go() {
    let (data, mime) = serialize_profile_export(&profile_export(), "application/yaml").unwrap();
    assert_eq!(mime, "application/yaml");
    assert_eq!(data, fixture("profile_with_icon.yaml"));
}

#[test]
fn profile_yaml_without_icon_matches_go() {
    let mut export = profile_export();
    export.icon_data.clear();
    let (data, _) = serialize_profile_export(&export, "application/yaml").unwrap();
    assert_eq!(data, fixture("profile_no_icon.yaml"));
}

#[test]
fn roundtrip_json_exports() {
    // Re-serializing a parsed Go fixture must reproduce it byte-for-byte.
    let original = fixture("settings.json");
    let parsed: SettingsExport = pm_sync::parse_export(&original, "application/json").unwrap().0;
    let (data, _) = serialize_export(&parsed, "application/json").unwrap();
    assert_eq!(data, original);

    let original = fixture("profile.json");
    let parsed: ProfileExport = pm_sync::parse_export(&original, "application/json").unwrap().0;
    let (data, _) = serialize_profile_export(&parsed, "application/json").unwrap();
    assert_eq!(data, original);
}

#[test]
fn roundtrip_yaml_exports() {
    let original = fixture("settings.yaml");
    let parsed: SettingsExport = pm_sync::parse_export(&original, "application/yaml").unwrap().0;
    let (data, _) = serialize_export(&parsed, "application/yaml").unwrap();
    assert_eq!(data, original);

    let original = fixture("profile_no_icon.yaml");
    let parsed: ProfileExport = pm_sync::parse_export(&original, "application/yaml").unwrap().0;
    let (data, _) = serialize_profile_export(&parsed, "application/yaml").unwrap();
    assert_eq!(data, original);
}
