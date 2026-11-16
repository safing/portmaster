//! Tests for the firewall configuration options (Go `firewall/config.go`).

use pm_config::Config;
use pm_firewall::{
    register_options, CFG_OPTION_ASK_TIMEOUT_KEY, CFG_OPTION_ASK_WITH_SYSTEM_NOTIFICATIONS_KEY,
    CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY, CFG_OPTION_ENABLE_FILTER_KEY,
    CFG_OPTION_PERMANENT_VERDICTS_KEY,
};

#[test]
fn options_match_go() {
    let config = Config::new();
    register_options(&config).expect("register options");

    let enable = config.export_option(CFG_OPTION_ENABLE_FILTER_KEY).expect("enable");
    assert_eq!(enable["Name"], "Enable Privacy Filter");
    assert_eq!(enable["OptType"], 4, "bool");
    assert_eq!(enable["DefaultValue"], true);
    assert_eq!(enable["ExpertiseLevel"], 2, "developer");
    assert_eq!(enable["ReleaseLevel"], 2, "experimental");
    assert_eq!(enable["Annotations"]["safing/portbase:ui:category"], "General");

    let permanent = config
        .export_option(CFG_OPTION_PERMANENT_VERDICTS_KEY)
        .expect("permanent verdicts");
    assert_eq!(permanent["Name"], "Permanent Verdicts");
    assert_eq!(permanent["Annotations"]["safing/portbase:ui:order"], 80);
    assert_eq!(
        permanent["Annotations"]["safing/portbase:ui:category"],
        "Advanced"
    );

    let dns = config
        .export_option(CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY)
        .expect("dns interception");
    assert_eq!(dns["Name"], "Seamless DNS Integration");
    assert_eq!(dns["Annotations"]["safing/portbase:ui:order"], 81);

    let prompts = config
        .export_option(CFG_OPTION_ASK_WITH_SYSTEM_NOTIFICATIONS_KEY)
        .expect("system notifications");
    assert_eq!(prompts["Name"], "Prompt Desktop Notifications");
    assert_eq!(prompts["ExpertiseLevel"], 0, "user");
    assert_eq!(prompts["ReleaseLevel"], 0, "stable");
    assert_eq!(prompts["Annotations"]["safing/portbase:ui:order"], 2);
    assert_eq!(
        prompts["Annotations"]["safing/portbase:config:requires"],
        serde_json::json!({"Key": "core/useSystemNotifications", "Value": true})
    );

    let timeout = config.export_option(CFG_OPTION_ASK_TIMEOUT_KEY).expect("timeout");
    assert_eq!(timeout["Name"], "Prompt Timeout");
    assert_eq!(timeout["OptType"], 3, "int");
    assert_eq!(timeout["DefaultValue"], 60);
    assert_eq!(timeout["Annotations"]["safing/portbase:ui:unit"], "seconds");
    assert_eq!(
        timeout["ValidationRegex"],
        "^[1-9][0-9]{1,5}$"
    );
}

#[test]
fn ask_timeout_validation() {
    let config = Config::new();
    register_options(&config).expect("register options");

    assert!(config
        .set_config_option(CFG_OPTION_ASK_TIMEOUT_KEY, serde_json::json!(60))
        .is_ok());
    assert!(config
        .set_config_option(CFG_OPTION_ASK_TIMEOUT_KEY, serde_json::json!(0))
        .is_err());
    assert!(config
        .set_config_option(CFG_OPTION_ASK_TIMEOUT_KEY, serde_json::json!(1234567))
        .is_err());
    assert!(config
        .set_config_option(CFG_OPTION_ASK_TIMEOUT_KEY, serde_json::json!(10))
        .is_ok());
}
