//! Value migrations (Go `MigrationFunc`), e.g. legacy security levels.

use pm_config::{
    migrate_security_level_to_boolean, Config, ConfigOption, OPT_TYPE_BOOL, OPT_TYPE_INT,
};
use serde_json::{json, Map, Value};

fn registered() -> std::sync::Arc<Config> {
    let config = Config::new();
    config
        .register(
            ConfigOption::new("Flag", "test/flag", "d", OPT_TYPE_BOOL, json!(false))
                .with_migration(migrate_security_level_to_boolean),
        )
        .unwrap();
    config
        .register(ConfigOption::new(
            "Plain",
            "test/plain",
            "d",
            OPT_TYPE_BOOL,
            json!(false),
        ))
        .unwrap();
    config
        .register(
            ConfigOption::new("Num", "test/num", "d", OPT_TYPE_INT, json!(0))
                .with_migration(migrate_security_level_to_boolean),
        )
        .unwrap();
    config
}

#[test]
fn migration_rule_matches_go() {
    let bool_option = ConfigOption::new("F", "k", "d", OPT_TYPE_BOOL, json!(false));
    let migrate = |v: Value| migrate_security_level_to_boolean(&bool_option, &v);
    assert_eq!(migrate(json!(0)), json!(false));
    assert_eq!(migrate(json!(1)), json!(true));
    assert_eq!(migrate(json!(2)), json!(false));
    assert_eq!(migrate(json!(3)), json!(true));
    assert_eq!(migrate(json!(4)), json!(false));
    assert_eq!(migrate(json!(7)), json!(true));
    // Truncated to 8 bits like Go uint8().
    assert_eq!(migrate(json!(257)), json!(true));
    assert_eq!(migrate(json!(256)), json!(false));
    assert_eq!(migrate(json!(-1)), json!(true));
    assert_eq!(migrate(json!(1.0)), json!(true));
    assert_eq!(migrate(json!(2.0)), json!(false));
    // Incompatible types stay as they are.
    assert_eq!(migrate(json!(true)), json!(true));
    assert_eq!(migrate(json!("1")), json!("1"));
    // Only boolean options are migrated.
    let int_option = ConfigOption::new("N", "k", "d", OPT_TYPE_INT, json!(0));
    assert_eq!(
        migrate_security_level_to_boolean(&int_option, &json!(7)),
        json!(7)
    );
}

#[test]
fn legacy_values_are_migrated_on_set_and_replace() {
    let config = registered();
    config
        .set_config_option("test/flag", json!(7))
        .expect("legacy int value is accepted");
    assert!(config.get_as_bool("test/flag", false)());
    config.set_config_option("test/flag", json!(2)).unwrap();
    assert!(!config.get_as_bool("test/flag", true)());

    // Without migration the legacy value is rejected.
    assert!(config.set_config_option("test/plain", json!(1)).is_err());

    let mut values = Map::new();
    values.insert("test/flag".to_string(), json!(5));
    let outcome = config.replace_config(&values);
    assert!(outcome.validation_errors.is_empty());
    assert!(config.get_as_bool("test/flag", false)());
}

#[test]
fn legacy_values_are_migrated_in_perspectives() {
    let config = registered();
    let (perspective, error) = config.perspective(&json!({"test": {"flag": 3, "plain": true}}));
    assert!(error.is_none());
    assert_eq!(perspective.get_bool("test/flag"), Some(true));
    assert_eq!(perspective.get_bool("test/plain"), Some(true));

    let (perspective, error) = config.perspective(&json!({"test": {"flag": 4}}));
    assert!(error.is_none());
    assert_eq!(perspective.get_bool("test/flag"), Some(false));

    // A non-boolean option keeps its value (migration is not applicable).
    let (perspective, error) = config.perspective(&json!({"test": {"num": 9}}));
    assert!(error.is_none());
    assert_eq!(perspective.get_int("test/num"), Some(9));
}
