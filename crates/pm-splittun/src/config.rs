//! Split tunnel configuration, port of `service/splittun/config.go`.

use serde_json::{json, Value};

use pm_config::{Config, ConfigOption};

/// Whether the split tunnel module is started.
pub const CFG_OPTION_SPLIT_TUN_ENABLE_KEY: &str = "splittun/enable";

/// Display order of the option.
const SPLIT_TUN_ENABLE_ORDER: i64 = 210;

/// Registers the split tunnel option.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    config.register(
        ConfigOption::new(
            "Split Tunnel Module",
            CFG_OPTION_SPLIT_TUN_ENABLE_KEY,
            "Start the Split Tunnel module. If turned off, the Split Tunnel functionality is fully disabled on this device.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(false),
        )
        .with_annotation(
            pm_config::DISPLAY_ORDER_ANNOTATION,
            json!(SPLIT_TUN_ENABLE_ORDER),
        )
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General")),
    )
}
