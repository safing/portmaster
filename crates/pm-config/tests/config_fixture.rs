//! Byte-compatibility tests for config.json and option exports.

use pm_config::{
    json_to_map, Config, ConfigOption, PossibleValue, CATEGORY_ANNOTATION, DISPLAY_HINT_ANNOTATION,
    DISPLAY_HINT_ONE_OF, DISPLAY_ORDER_ANNOTATION, EXPERTISE_LEVEL_EXPERT, EXPERTISE_LEVEL_USER,
    OPT_TYPE_BOOL, OPT_TYPE_INT, OPT_TYPE_STRING, OPT_TYPE_STRING_ARRAY, RELEASE_LEVEL_BETA,
    RELEASE_LEVEL_STABLE,
};
use serde_json::{json, Value};

fn fixture() -> Value {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/config.json");
    let text = std::fs::read_to_string(path).unwrap();
    serde_json::from_str(&text).unwrap()
}

fn test_options() -> Vec<ConfigOption> {
    vec![
        ConfigOption {
            name: "String Option".to_string(),
            key: "test/string".to_string(),
            description: "A string option.".to_string(),
            help: "Some help text.".to_string(),
            sensitive: true,
            opt_type: OPT_TYPE_STRING,
            expertise_level: EXPERTISE_LEVEL_EXPERT,
            release_level: RELEASE_LEVEL_STABLE,
            default_value: json!("default"),
            validation_regex: "^(default|banana|water)$".to_string(),
            possible_values: vec![
                PossibleValue {
                    name: "Default".to_string(),
                    value: json!("default"),
                    description: "The default.".to_string(),
                },
                PossibleValue {
                    name: "Banana".to_string(),
                    value: json!("banana"),
                    description: "A fruit.".to_string(),
                },
                PossibleValue {
                    name: "Water".to_string(),
                    value: json!("water"),
                    description: "A drink.".to_string(),
                },
            ],
            annotations: Some(
                json!({
                    DISPLAY_HINT_ANNOTATION: DISPLAY_HINT_ONE_OF,
                    DISPLAY_ORDER_ANNOTATION: 10,
                    CATEGORY_ANNOTATION: "Test",
                })
                .as_object()
                .cloned()
                .unwrap(),
            ),
            ..ConfigOption::default()
        },
        ConfigOption {
            name: "String Array Option".to_string(),
            key: "test/stringArray".to_string(),
            description: "A string array option.".to_string(),
            opt_type: OPT_TYPE_STRING_ARRAY,
            expertise_level: EXPERTISE_LEVEL_USER,
            release_level: RELEASE_LEVEL_BETA,
            default_value: json!(["default"]),
            ..ConfigOption::default()
        },
        ConfigOption {
            name: "Int Option".to_string(),
            key: "test/int".to_string(),
            description: "An int option.".to_string(),
            opt_type: OPT_TYPE_INT,
            default_value: json!(7),
            ..ConfigOption::default()
        },
        ConfigOption {
            name: "Bool Option".to_string(),
            key: "test/bool".to_string(),
            description: "A bool option.".to_string(),
            opt_type: OPT_TYPE_BOOL,
            default_value: json!(false),
            ..ConfigOption::default()
        },
        ConfigOption {
            name: "Default Option".to_string(),
            key: "test/withDefault".to_string(),
            description: "An option with an internal default.".to_string(),
            opt_type: OPT_TYPE_STRING,
            default_value: json!("fallback"),
            ..ConfigOption::default()
        },
    ]
}

fn temp_dir(tag: &str) -> std::path::PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!(
        "pm-config-{tag}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos()
    ));
    let _ = std::fs::remove_dir_all(&path);
    std::fs::create_dir_all(&path).unwrap();
    path
}

fn go_json(value: &Value) -> String {
    String::from_utf8(pm_dsd::gojson::to_vec(value).unwrap()).unwrap()
}

#[test]
fn config_file_and_exports_match_go() {
    let fixture = fixture();
    let dir = temp_dir("fixture");
    let path = dir.join("config.json");

    let config = Config::new();
    config.set_config_file(&path);
    for option in test_options() {
        config.register(option).unwrap();
    }

    // Same values as the Go fixture generator; the beta option's value is kept
    // in memory but not persisted, like in Go.
    config
        .set_config_option("test/string", json!("banana"))
        .unwrap();
    config
        .set_config_option("test/stringArray", json!(["x<&>", "y"]))
        .unwrap();
    config.set_config_option("test/int", json!(42)).unwrap();
    config.set_config_option("test/bool", json!(true)).unwrap();
    config
        .set_default_config_option("test/withDefault", json!("internal"))
        .unwrap();

    let written = std::fs::read_to_string(&path).unwrap();
    assert_eq!(written, fixture["file"].as_str().unwrap());

    let flattened = json_to_map(written.as_bytes()).unwrap();
    assert_eq!(Value::Object(flattened), fixture["flattened"]);

    for (key, expected) in fixture["exports"].as_object().unwrap() {
        let export = config.export_option(key).unwrap();
        assert_eq!(
            go_json(&export),
            expected.as_str().unwrap(),
            "export of {key}"
        );
    }

    for definition in config
        .export_options()
        .iter()
        .filter(|definition| definition.key.starts_with("test/"))
    {
        let expected = fixture["definitions"]
            .as_array()
            .unwrap()
            .iter()
            .find(|candidate| candidate["Key"] == definition.key)
            .unwrap_or_else(|| panic!("no Go definition for {}", definition.key));
        assert_eq!(
            go_json(&serde_json::to_value(definition).unwrap()),
            go_json(expected),
            "definition of {}",
            definition.key
        );
    }
}

#[test]
fn config_file_round_trips() {
    let fixture = fixture();
    let dir = temp_dir("roundtrip");
    let path = dir.join("config.json");
    std::fs::write(&path, fixture["file"].as_str().unwrap()).unwrap();

    let config = Config::new();
    for option in test_options() {
        config.register(option).unwrap();
    }
    let outcome = config.load_config(&path).unwrap();
    assert!(outcome.validation_errors.is_empty());
    assert!(!outcome.contains_unknown);

    config.set_config_option("test/int", json!(43)).unwrap();
    let saved = std::fs::read_to_string(&path).unwrap();
    assert_eq!(
        saved,
        "{\n  \"test\": {\n    \"bool\": true,\n    \"int\": 43,\n    \"string\": \"banana\"\n  }\n}"
    );
}
