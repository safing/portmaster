//! Behavior tests of the config system, ported from the Go package tests plus
//! database, event and persistence coverage.

use std::sync::Arc;

use pm_config::{
    json_to_map, Config, ConfigOption, Error, Perspective, CATEGORY_ANNOTATION,
    DISPLAY_HINT_ANNOTATION, DISPLAY_HINT_ONE_OF, EXPERTISE_LEVEL_DEVELOPER, EXPERTISE_LEVEL_USER,
    OPT_TYPE_BOOL, OPT_TYPE_INT, OPT_TYPE_STRING, OPT_TYPE_STRING_ARRAY, RELEASE_LEVEL_BETA,
    RELEASE_LEVEL_EXPERIMENTAL, RELEASE_LEVEL_KEY, RELEASE_LEVEL_STABLE,
    RESTART_PENDING_ANNOTATION,
};
use pm_database::Databases;
use serde_json::{json, Map, Value};

fn temp_dir(tag: &str) -> std::path::PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!(
        "pm-config-api-{tag}-{}-{}",
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

fn values(json_text: &str) -> Map<String, Value> {
    json_to_map(json_text.as_bytes()).unwrap()
}

fn quick_register(config: &Config, key: &str, opt_type: u8, default: Value) {
    config
        .register(ConfigOption::new(
            key,
            key,
            "test config",
            opt_type,
            default,
        ))
        .unwrap();
}

#[test]
fn getters_resolve_layers() {
    let config = Config::new();
    quick_register(&config, "monkey", OPT_TYPE_STRING, json!("c"));
    quick_register(
        &config,
        "zebras/zebra",
        OPT_TYPE_STRING_ARRAY,
        json!(["a", "b"]),
    );
    quick_register(&config, "elephant", OPT_TYPE_INT, json!(-1));
    quick_register(&config, "hot", OPT_TYPE_BOOL, json!(false));
    quick_register(&config, "cold", OPT_TYPE_BOOL, json!(true));

    let monkey = config.get_as_string("monkey", "none");
    let zebra = config.get_as_string_array("zebras/zebra", &[]);
    let elephant = config.get_as_int("elephant", -1);
    let hot = config.get_as_bool("hot", false);
    let cold = config.get_as_bool("cold", true);

    // Registration defaults.
    assert_eq!(monkey(), "c");
    assert_eq!(zebra(), vec!["a", "b"]);
    assert_eq!(elephant(), -1);
    assert!(!hot());
    assert!(cold());

    let outcome = config.replace_config(&values(
        r#"{
            "monkey": "a",
            "zebras": {"zebra": ["black", "white"]},
            "elephant": 2,
            "hot": true,
            "cold": false
        }"#,
    ));
    assert!(outcome.validation_errors.is_empty());
    assert_eq!(monkey(), "a");
    assert_eq!(zebra(), vec!["black", "white"]);
    assert_eq!(elephant(), 2);
    assert!(hot());
    assert!(!cold());

    // Internal defaults fill gaps but never override user values.
    let outcome =
        config.replace_default_config(&values(r#"{"monkey": "b", "elephant": 0, "snake": "0"}"#));
    assert!(outcome.contains_unknown);
    assert_eq!(monkey(), "a");
    assert_eq!(elephant(), 2);

    // Replacing the user config refreshes cached getters.
    config.replace_config(&values(r#"{"monkey": "3"}"#));
    assert_eq!(monkey(), "3");
    assert_eq!(elephant(), 0);

    // Type mismatches fall back.
    assert_eq!(config.get_as_string("elephant", "fallback")(), "fallback");
    assert_eq!(config.get_as_int("monkey", -1)(), -1);
    assert_eq!(config.get_as_string("missing", "fallback")(), "fallback");
    assert!(!config.get_as_bool("missing", false)());
}

#[test]
fn setters_validate_and_persist() {
    let dir = temp_dir("setters");
    let path = dir.join("config.json");
    let config = Config::new();
    config.set_config_file(&path);

    config
        .register(
            ConfigOption::new(
                "name",
                "monkey",
                "description",
                OPT_TYPE_STRING,
                json!("banana"),
            )
            .with_validation_regex("^(banana|water)$"),
        )
        .unwrap();
    config
        .register(
            ConfigOption::new(
                "name",
                "zebras/zebra",
                "description",
                OPT_TYPE_STRING_ARRAY,
                json!(["black", "white"]),
            )
            .with_validation_regex("^[a-z]+$"),
        )
        .unwrap();
    config
        .register(
            ConfigOption::new("name", "elephant", "description", OPT_TYPE_INT, json!(2))
                .with_requires_restart(true),
        )
        .unwrap();
    config
        .register(ConfigOption::new(
            "name",
            "hot",
            "description",
            OPT_TYPE_BOOL,
            json!(true),
        ))
        .unwrap();

    // Correct types.
    config.set_config_option("monkey", json!("banana")).unwrap();
    config
        .set_config_option("zebras/zebra", json!(["black", "white"]))
        .unwrap();
    config
        .set_default_config_option("elephant", json!(3))
        .unwrap();
    config
        .set_default_config_option("hot", json!(true))
        .unwrap();

    // Wrong types.
    assert!(config
        .set_config_option("monkey", json!(["black", "white"]))
        .is_err());
    assert!(config.set_config_option("zebras/zebra", json!(2)).is_err());
    assert!(config
        .set_default_config_option("elephant", json!(true))
        .is_err());
    assert!(config
        .set_default_config_option("hot", json!("banana"))
        .is_err());

    // Validation failures.
    let err = config
        .set_config_option("monkey", json!("dirt"))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "validation of monkey failed: did not match validation regex"
    );
    let err = config
        .set_config_option("zebras/zebra", json!(["Element649"]))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "validation of zebras/zebra failed: entry #1 did not match validation regex"
    );

    // Unknown options.
    assert!(matches!(
        config.set_config_option("invalid", json!("banana")),
        Err(Error::UnknownOption(_))
    ));
    assert!(matches!(
        config.unset_config_option("invalid_delete"),
        Err(Error::UnknownOption(_))
    ));

    // The restart-pending annotation is set for options requiring a restart.
    let elephant = config.option("elephant").unwrap();
    assert_eq!(
        elephant.annotation(RESTART_PENDING_ANNOTATION),
        Some(Value::Bool(true))
    );

    // Only user values are persisted, and unsetting removes them.
    let saved = std::fs::read_to_string(&path).unwrap();
    assert_eq!(
        saved,
        "{\n  \"monkey\": \"banana\",\n  \"zebras\": {\n    \"zebra\": [\n      \"black\",\n      \"white\"\n    ]\n  }\n}"
    );

    config.unset_config_option("monkey").unwrap();
    assert_eq!(config.get_as_string("monkey", "none")(), "banana");
    let saved = std::fs::read_to_string(&path).unwrap();
    assert!(!saved.contains("monkey"), "{saved}");
}

#[test]
fn validation_messages_match_go() {
    let config = Config::new();
    config
        .register(ConfigOption::new(
            "Int",
            "test/int",
            "test",
            OPT_TYPE_INT,
            json!(1),
        ))
        .unwrap();
    config
        .register(
            ConfigOption::new(
                "List",
                "test/list",
                "test",
                OPT_TYPE_STRING_ARRAY,
                json!(["a"]),
            )
            .with_validation_regex("^[a-z]+$"),
        )
        .unwrap();

    let err = config
        .set_config_option("test/int", json!("text"))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "validation of test/int failed: expected type int, got type string"
    );

    let err = config
        .set_config_option("test/int", json!(1.5))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "validation of test/int failed: failed to convert float64 to int64"
    );

    let err = config
        .set_config_option("test/list", json!(["a", 2]))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "validation of test/list failed: entry #2 is not a string"
    );

    let err = config
        .set_config_option("test/list", json!(["a", "B"]))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "validation of test/list failed: entry #2 did not match validation regex"
    );

    let outcome = config.validate_config(&values(r#"{"test/int": 5}"#));
    assert!(outcome.validation_errors.is_empty());

    let outcome = config.validate_config(&values(r#"{"test/int": "x"}"#));
    assert_eq!(outcome.validation_errors.len(), 1);
}

#[test]
fn registration_errors() {
    let config = Config::new();

    let err = config
        .register(ConfigOption::new(
            "",
            "key",
            "d",
            OPT_TYPE_STRING,
            json!(""),
        ))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "failed to register option: please set option.Name"
    );

    let err = config
        .register(ConfigOption::new(
            "name",
            "",
            "d",
            OPT_TYPE_STRING,
            json!(""),
        ))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "failed to register option: please set option.Key"
    );

    let err = config
        .register(ConfigOption::new(
            "name",
            "key",
            "",
            OPT_TYPE_STRING,
            json!(""),
        ))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "failed to register option: please set option.Description"
    );

    let err = config
        .register(ConfigOption::new("name", "key", "d", 0, json!("")))
        .unwrap_err();
    assert_eq!(
        err.to_string(),
        "failed to register option: please set option.OptType"
    );

    let err = config
        .register(
            ConfigOption::new("name", "key", "d", OPT_TYPE_STRING, json!("x"))
                .with_validation_regex("["),
        )
        .unwrap_err();
    assert!(err.to_string().contains("could not compile"), "{err}");

    let err = config
        .register(ConfigOption::new(
            "name",
            "key",
            "d",
            OPT_TYPE_INT,
            json!("not an int"),
        ))
        .unwrap_err();
    assert!(err.to_string().contains("invalid default value"), "{err}");

    // Possible values generate a validation regex, like Go.
    config
        .register(
            ConfigOption::new("name", "choice", "d", OPT_TYPE_STRING, json!("a"))
                .with_possible_values(vec![
                    pm_config::PossibleValue {
                        name: "A".to_string(),
                        value: json!("a"),
                        description: String::new(),
                    },
                    pm_config::PossibleValue {
                        name: "B".to_string(),
                        value: json!("b"),
                        description: String::new(),
                    },
                ]),
        )
        .unwrap();
    assert_eq!(
        config
            .option("choice")
            .unwrap()
            .definition()
            .validation_regex,
        "^(a|b)$"
    );

    // Options cannot be registered after initialization.
    config.mark_initialized();
    assert!(matches!(
        config.register(ConfigOption::new(
            "Late",
            "late",
            "d",
            OPT_TYPE_STRING,
            json!("")
        )),
        Err(Error::AlreadyInitialized(_))
    ));
}

#[test]
fn release_levels_filter_options() {
    let config = Config::new();
    config
        .register(
            ConfigOption::new(
                "Stable",
                "subsystem/stable",
                "d",
                OPT_TYPE_BOOL,
                json!(false),
            )
            .with_release_level(RELEASE_LEVEL_STABLE),
        )
        .unwrap();
    config
        .register(
            ConfigOption::new("Beta", "subsystem/beta", "d", OPT_TYPE_BOOL, json!(false))
                .with_release_level(RELEASE_LEVEL_BETA),
        )
        .unwrap();
    config
        .register(
            ConfigOption::new(
                "Experimental",
                "subsystem/experimental",
                "d",
                OPT_TYPE_BOOL,
                json!(false),
            )
            .with_release_level(RELEASE_LEVEL_EXPERIMENTAL),
        )
        .unwrap();

    for key in [
        "subsystem/stable",
        "subsystem/beta",
        "subsystem/experimental",
    ] {
        config.set_config_option(key, json!(true)).unwrap();
    }

    let stable = config.get_as_bool("subsystem/stable", false);
    let beta = config.get_as_bool("subsystem/beta", false);
    let experimental = config.get_as_bool("subsystem/experimental", false);

    assert!(stable());
    assert!(!beta());
    assert!(!experimental());
    assert_eq!(config.release_level(), RELEASE_LEVEL_STABLE);

    config
        .set_config_option(RELEASE_LEVEL_KEY, json!("beta"))
        .unwrap();
    assert_eq!(config.release_level(), RELEASE_LEVEL_BETA);
    assert!(beta());
    assert!(!experimental());

    config
        .set_config_option(RELEASE_LEVEL_KEY, json!("experimental"))
        .unwrap();
    assert_eq!(config.release_level(), RELEASE_LEVEL_EXPERIMENTAL);
    assert!(experimental());

    // Only stable values are persisted while the level is stable.
    config
        .set_config_option(RELEASE_LEVEL_KEY, json!("stable"))
        .unwrap();
    let active = config.active_config_values();
    assert!(active.contains_key("subsystem/stable"));
    assert!(!active.contains_key("subsystem/beta"));

    config
        .set_config_option("core/expertiseLevel", json!("developer"))
        .unwrap();
    assert_eq!(config.expertise_level(), EXPERTISE_LEVEL_DEVELOPER);
}

#[test]
fn perspectives_read_values_without_changing_config() {
    let config = Config::new();
    quick_register(&config, "monkey", OPT_TYPE_STRING, json!("c"));
    quick_register(
        &config,
        "zebras/zebra",
        OPT_TYPE_STRING_ARRAY,
        json!(["a", "b"]),
    );
    quick_register(&config, "elephant", OPT_TYPE_INT, json!(-1));
    quick_register(&config, "hot", OPT_TYPE_BOOL, json!(false));
    quick_register(&config, "cold", OPT_TYPE_BOOL, json!(true));

    let nested = json!({
        "monkey": "a",
        "zebras": {"zebra": ["black", "white"]},
        "elephant": 2,
        "hot": true,
        "cold": false
    });

    let (perspective, error) = config.perspective(&nested);
    assert!(error.is_none());
    assert_eq!(perspective.get_string("monkey").as_deref(), Some("a"));
    assert_eq!(
        perspective.get_string_array("zebras/zebra"),
        Some(vec!["black".to_string(), "white".to_string()])
    );
    assert_eq!(perspective.get_int("elephant"), Some(2));
    assert_eq!(perspective.get_bool("hot"), Some(true));
    assert_eq!(perspective.get_bool("cold"), Some(false));
    assert!(!perspective.has("missing"));

    // Type mismatches return None, like Go.
    assert_eq!(perspective.get_string("elephant"), None);

    // Invalid values produce an error but keep the valid ones.
    let (perspective, error) = config.perspective(&json!({
        "monkey": 3,
        "elephant": 2
    }));
    let error = error.expect("validation error");
    assert!(
        error.to_string().contains("encountered 1 errors"),
        "{error}"
    );
    assert_eq!(perspective.get_int("elephant"), Some(2));

    // The perspective does not touch the global configuration.
    assert_eq!(config.get_as_string("monkey", "none")(), "c");
}

#[test]
fn unknown_keys_survive_save() {
    // Deliberate deviation: Go's SaveConfig drops keys of unregistered options,
    // this port keeps them so that no user data is lost.
    let dir = temp_dir("unknown");
    let path = dir.join("config.json");
    std::fs::write(
        &path,
        "{\n  \"known\": \"a\",\n  \"unknown\": {\n    \"key\": 5\n  }\n}",
    )
    .unwrap();

    let config = Config::new();
    quick_register(&config, "known", OPT_TYPE_STRING, json!("default"));
    let outcome = config.load_config(&path).unwrap();
    assert!(outcome.contains_unknown);
    assert_eq!(config.unknown_values()["unknown/key"], json!(5));

    config.set_config_option("known", json!("b")).unwrap();
    let saved = std::fs::read_to_string(&path).unwrap();
    assert_eq!(
        saved,
        "{\n  \"known\": \"b\",\n  \"unknown\": {\n    \"key\": 5\n  }\n}"
    );
}

#[test]
fn database_exposes_options_and_events() {
    let dir = temp_dir("database");
    let path = dir.join("config.json");
    let config = Config::new();
    config.set_config_file(&path);
    quick_register(&config, "test/string", OPT_TYPE_STRING, json!("default"));
    quick_register(&config, "test/int", OPT_TYPE_INT, json!(1));

    let dbs = Arc::new(Databases::without_disk());
    config.register_database(&dbs).unwrap();
    let internal = dbs.internal_interface();

    // The config database serves exported options.
    let record = internal.get("config:test/string").unwrap();
    let accessor = record.accessor().unwrap();
    assert_eq!(accessor.get_string("Key").as_deref(), Some("test/string"));
    assert_eq!(
        accessor.get_string("DefaultValue").as_deref(),
        Some("default")
    );

    // Writing a Value through the database changes the option.
    let mut record = record;
    let mut accessor = record.accessor().unwrap();
    accessor.set("Value", json!("water")).unwrap();
    record.data = pm_dsd::gojson::to_vec(accessor.value()).unwrap();
    internal.put(record).unwrap();
    assert_eq!(config.get_as_string("test/string", "none")(), "water");

    // Queries list options by prefix, sorted.
    let query = pm_database::Query::new("config:");
    let records = internal.query(&query).unwrap();
    assert!(records.len() >= 2);
    assert!(records
        .windows(2)
        .all(|pair| pair[0].key.key <= pair[1].key.key));

    // Subscriptions receive pushed updates.
    let mut subscription = internal.subscribe(query.clone()).unwrap();
    config.set_config_option("test/int", json!(5)).unwrap();
    let update = subscription.try_recv().expect("config update");
    assert_eq!(update.key.key, "test/int");

    // Change events are broadcast.
    let mut changes = config.subscribe_changes();
    config.set_config_option("test/int", json!(6)).unwrap();
    assert!(changes.try_recv().is_ok());

    // Deleting through the database unsets the value.
    internal.delete("config:test/int").unwrap();
    assert_eq!(config.get_as_int("test/int", -1)(), 1);
}

#[test]
fn storage_rejects_wrong_value_types() {
    let config = Config::new();
    quick_register(&config, "test/int", OPT_TYPE_INT, json!(1));

    let dbs = Arc::new(Databases::without_disk());
    config.register_database(&dbs).unwrap();
    let internal = dbs.internal_interface();

    let mut record = internal.get("config:test/int").unwrap();
    let mut accessor = record.accessor().unwrap();
    accessor.set("Value", json!("text")).unwrap();
    record.data = pm_dsd::gojson::to_vec(accessor.value()).unwrap();
    let err = internal.put(record).unwrap_err();
    assert!(
        err.to_string()
            .contains("received invalid value in \"Value\""),
        "{err}"
    );
}

#[test]
fn perspective_type_alias_is_public() {
    // Keeps the exported type name referenced for API users.
    fn assert_perspective(_: &Perspective) {}
    let config = Config::new();
    let (perspective, _) = config.perspective(&json!({}));
    assert_perspective(&perspective);
}

#[test]
fn option_annotations_are_mutable() {
    let config = Config::new();
    config
        .register(
            ConfigOption::new(
                "Annotated",
                "test/annotated",
                "d",
                OPT_TYPE_STRING,
                json!("a"),
            )
            .with_annotation(DISPLAY_HINT_ANNOTATION, json!(DISPLAY_HINT_ONE_OF))
            .with_annotation(CATEGORY_ANNOTATION, json!("Test"))
            .with_expertise_level(EXPERTISE_LEVEL_USER),
        )
        .unwrap();

    let option = config.option("test/annotated").unwrap();
    option.set_annotation("custom", json!(1));
    assert_eq!(option.annotation("custom"), Some(json!(1)));
    assert_eq!(
        option.annotation(DISPLAY_HINT_ANNOTATION),
        Some(json!("one-of"))
    );
    assert_eq!(
        config.export_option("test/annotated").unwrap()["Annotations"]["custom"],
        json!(1)
    );
}
