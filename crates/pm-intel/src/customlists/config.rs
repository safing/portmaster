//! Configuration option of the custom filter list.

use serde_json::{json, Value};

use pm_config::{Config, ConfigOption};

/// Key of the custom filter list file option.
pub const CFG_OPTION_CUSTOM_LIST_FILE_KEY: &str = "filter/customListFile";
/// Display order of the option in the UI.
const CFG_OPTION_CUSTOM_LIST_FILE_ORDER: i64 = 35;
/// Category annotation value of the option.
const CFG_OPTION_CUSTOM_LIST_CATEGORY_ANNOTATION: &str = "Filter Lists";

/// Help text of the option, byte-identical to Go (including the trailing
/// double spaces that start a markdown line break).
const HELP: &str = "The file (.txt) is checked every couple minutes and will be automatically reloaded when it has changed.  \n\nEntries (one per line) may be one of:\n- Domain: \"example.com\"\n- IP Address: \"10.0.0.1\"\n- Country Code (based on IP): \"US\"\n- AS (Autonomous System): \"AS1234\"  \n\nEverything after the first element of a line, comments starting with a '#', and empty lines are ignored.  \nThe settings \"Block Subdomains of Filter List Entries\" and \"Block Domain Aliases\" also apply to the custom filter list.  \nLists in the \"Hosts\" format are not supported.  \n\nPlease note that the custom filter list is fully loaded into memory. This can have a negative impact on your device if big lists are loaded.";

/// Registers the configuration option of the custom filter list.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    let mut option = ConfigOption::new(
        "Custom Filter List",
        CFG_OPTION_CUSTOM_LIST_FILE_KEY,
        "Specify the file path to a custom filter list (.txt), which will be automatically refreshed. Any connections matching a domain, IP address, Country or ASN in the file will be blocked.",
        pm_config::OPT_TYPE_STRING,
        Value::String(String::new()),
    );
    option.help = HELP.to_string();
    option.expertise_level = pm_config::EXPERTISE_LEVEL_EXPERT;
    option.release_level = pm_config::RELEASE_LEVEL_STABLE;
    option.requires_restart = false;

    config.register(
        option
            .with_annotation(
                pm_config::DISPLAY_ORDER_ANNOTATION,
                json!(CFG_OPTION_CUSTOM_LIST_FILE_ORDER),
            )
            .with_annotation(
                pm_config::CATEGORY_ANNOTATION,
                json!(CFG_OPTION_CUSTOM_LIST_CATEGORY_ANNOTATION),
            )
            .with_annotation(
                pm_config::DISPLAY_HINT_ANNOTATION,
                json!(pm_config::DISPLAY_HINT_FILE_PICKER),
            ),
    )
}

/// Returns a getter for the configured custom filter list file path.
pub fn get_file_path(config: &std::sync::Arc<Config>) -> impl Fn() -> String {
    config.get_as_string(CFG_OPTION_CUSTOM_LIST_FILE_KEY, "")
}
