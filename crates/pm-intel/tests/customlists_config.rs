//! Tests for the custom filter list configuration option (Go
//! `service/intel/customlists/config.go`).

use pm_config::Config;
use pm_intel::customlists::{register_options, CFG_OPTION_CUSTOM_LIST_FILE_KEY};

#[test]
fn registers_option_like_go() {
    let config = Config::new();
    register_options(&config).expect("register custom list option");

    let option = config
        .export_option(CFG_OPTION_CUSTOM_LIST_FILE_KEY)
        .expect("exported option");

    assert_eq!(option["Name"], "Custom Filter List");
    assert_eq!(option["Key"], CFG_OPTION_CUSTOM_LIST_FILE_KEY);
    assert_eq!(
        option["Description"],
        "Specify the file path to a custom filter list (.txt), which will be automatically refreshed. Any connections matching a domain, IP address, Country or ASN in the file will be blocked."
    );
    // Go `config.OptTypeString`.
    assert_eq!(option["OptType"], 1);
    // Go `config.ExpertiseLevelExpert` and `config.ReleaseLevelStable`.
    assert_eq!(option["ExpertiseLevel"], 1);
    assert_eq!(option["ReleaseLevel"], 0);
    assert_eq!(option["RequiresRestart"], false);
    assert_eq!(option["DefaultValue"], "");
    assert_eq!(
        option["Annotations"]["safing/portbase:ui:order"],
        35,
        "display order"
    );
    assert_eq!(
        option["Annotations"]["safing/portbase:ui:category"],
        "Filter Lists"
    );
    assert_eq!(
        option["Annotations"]["safing/portbase:ui:display-hint"],
        "file-picker"
    );
    assert!(
        option["Help"]
            .as_str()
            .expect("help is a string")
            .starts_with("The file (.txt) is checked every couple minutes"),
        "help text is set"
    );
}

#[test]
fn default_value_is_empty_and_getter_reads_it() {
    let config = Config::new();
    register_options(&config).expect("register custom list option");

    let get_path = pm_intel::customlists::get_file_path(&config);
    assert_eq!(get_path(), "");

    config
        .set_config_option(
            CFG_OPTION_CUSTOM_LIST_FILE_KEY,
            serde_json::json!("/etc/portmaster/custom.txt"),
        )
        .expect("set option");
    assert_eq!(get_path(), "/etc/portmaster/custom.txt");
}
