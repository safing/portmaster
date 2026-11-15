use pm_config::Config;
use pm_profile::options::*;
use pm_profile::{GlobalProfileConfig, DEFAULT_ACTION_BLOCK, DEFAULT_ACTION_PERMIT};
use serde_json::{json, Value};

fn go_json(value: &Value) -> String {
    String::from_utf8(pm_dsd::gojson::to_vec(value).unwrap()).unwrap()
}

fn registered() -> std::sync::Arc<Config> {
    let config = Config::new();
    register_options(&config).unwrap();
    config
}

#[test]
fn options_match_go_export() {
    let path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../fixtures/m0/profile_options.json"
    );
    let expected: Value = serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap();
    let expected = expected.as_array().unwrap();

    let config = registered();
    let ours = config.export_options();
    // The config registers its own level options; only profile keys are compared.
    let ours: Vec<_> = ours
        .iter()
        .filter(|o| {
            ["filter/", "history/", "spn/", "splittun/"]
                .iter()
                .any(|p| o.key.starts_with(p))
        })
        .collect();
    assert_eq!(ours.len(), expected.len());
    for (ours, expected) in ours.iter().zip(expected) {
        assert_eq!(
            go_json(&serde_json::to_value(ours).unwrap()),
            go_json(expected),
            "option {}",
            ours.key
        );
    }
}

#[test]
fn endpoint_list_validation() {
    let config = registered();
    assert!(config
        .set_config_option(CFG_OPTION_ENDPOINTS_KEY, json!(["+ example.com", "- *"]))
        .is_ok());
    assert!(config
        .set_config_option(CFG_OPTION_ENDPOINTS_KEY, json!(["+ 10.0.0.0/33"]))
        .is_err());
    assert!(config
        .set_config_option(CFG_OPTION_ENDPOINTS_KEY, json!(["bad rule"]))
        .is_err());
}

#[test]
fn default_action_and_other_validation() {
    let config = registered();
    for ok in ["permit", "block", "ask"] {
        assert!(config
            .set_config_option(CFG_OPTION_DEFAULT_ACTION_KEY, json!(ok))
            .is_ok());
    }
    assert!(config
        .set_config_option(CFG_OPTION_DEFAULT_ACTION_KEY, json!("nope"))
        .is_err());
    assert!(config
        .set_config_option(CFG_OPTION_FILTER_LISTS_KEY, json!(["MAL", "a b"]))
        .is_err());
    assert!(config
        .set_config_option(CFG_OPTION_SPLIT_TUN_INTERFACE_KEY, json!("  "))
        .is_err());
    assert!(config
        .set_config_option(CFG_OPTION_SPLIT_TUN_INTERFACE_KEY, json!("wlan0"))
        .is_ok());
    for good in [
        "socks5://127.0.0.1:1080",
        "http://u:p@h:3128",
        "socks4://id@h:1",
        "http://h:80",
        "",
    ] {
        assert!(
            config
                .set_config_option(CFG_OPTION_SPLIT_TUN_PROXY_KEY, json!(good))
                .is_ok(),
            "{good}"
        );
    }
    for bad in [
        "ftp://h:1",
        "socks5://h",
        "socks5://h:1/x",
        "socks4://u:p@h:1",
        "http://:3",
        "nonsense",
    ] {
        assert!(
            config
                .set_config_option(CFG_OPTION_SPLIT_TUN_PROXY_KEY, json!(bad))
                .is_err(),
            "{bad}"
        );
    }
}

#[test]
fn global_config_refreshes() {
    let config = registered();
    let global = GlobalProfileConfig::new(&config);
    global.refresh().unwrap();
    assert_eq!(global.default_action_value(), DEFAULT_ACTION_PERMIT);
    assert_eq!(global.filter_lists(), ["TRAC", "MAL", "BAD", "UNBREAK"]);
    assert!(!global.endpoints().is_set());
    // Registered default is true (the Go getter fallback false is never used).
    assert!((global.block_inbound)());

    config
        .set_config_option(CFG_OPTION_DEFAULT_ACTION_KEY, json!("block"))
        .unwrap();
    config
        .set_config_option(CFG_OPTION_ENDPOINTS_KEY, json!(["- example.com"]))
        .unwrap();
    global.refresh().unwrap();
    assert_eq!(global.default_action_value(), DEFAULT_ACTION_BLOCK);
    assert!(global.endpoints().is_set());
    assert_eq!(global.keep_history.as_ref()(), 30);
}
