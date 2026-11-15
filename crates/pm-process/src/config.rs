//! Configuration option of the process module.

use serde_json::json;

use pm_config::{Config, ConfigOption};
use serde_json::Value;

/// Key of the process detection option.
pub const CFG_OPTION_ENABLE_PROCESS_DETECTION_KEY: &str = "core/enableProcessDetection";

/// Registers the configuration options of the process module.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    config.register(
        ConfigOption::new(
            "Process Detection",
            CFG_OPTION_ENABLE_PROCESS_DETECTION_KEY,
            "This option enables the attribution of network traffic to processes. Without it, app settings are effectively disabled.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(528))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )
}

/// Returns a getter for the process detection option.
pub fn enabled_getter(config: &std::sync::Arc<Config>) -> impl Fn() -> bool {
    config.get_as_bool(CFG_OPTION_ENABLE_PROCESS_DETECTION_KEY, true)
}
