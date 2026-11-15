//! Configuration option of the notifications package.

use serde_json::json;

use pm_config::{Config, ConfigOption};

/// Key of the system notifications option.
pub const CFG_USE_SYSTEM_NOTIFICATIONS_KEY: &str = "core/useSystemNotifications";

/// Registers the configuration options of the notifications package.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    config.register(
        ConfigOption::new(
            "Desktop Notifications",
            CFG_USE_SYSTEM_NOTIFICATIONS_KEY,
            "In addition to showing notifications in the Portmaster App, also send them to the Desktop. This requires the Portmaster Notifier to be running.",
            pm_config::OPT_TYPE_BOOL,
            json!(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_USER)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(-15))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("User Interface")),
    )
}
