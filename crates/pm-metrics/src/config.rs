//! Configuration options of the metrics module.

use std::sync::Arc;

use pm_config::{
    Config, ConfigOption, CATEGORY_ANNOTATION, DISPLAY_ORDER_ANNOTATION, EXPERTISE_LEVEL_EXPERT,
    OPT_TYPE_STRING, RELEASE_LEVEL_STABLE,
};
use serde_json::{json, Value};

use crate::metric::valid_prometheus_name;

pub const CFG_OPTION_INSTANCE_KEY: &str = "core/metrics/instance";
pub const CFG_OPTION_COMMENT_KEY: &str = "core/metrics/comment";
pub const CFG_OPTION_PUSH_KEY: &str = "core/metrics/push";

/// Reads the host name like Go `os.Hostname`, without external dependencies.
fn hostname() -> String {
    std::fs::read_to_string("/proc/sys/kernel/hostname")
        .or_else(|_| std::fs::read_to_string("/etc/hostname"))
        .unwrap_or_default()
        .trim()
        .to_string()
}

/// The default `instance` label: the host name with dashes removed.
pub fn default_instance() -> String {
    let instance = hostname().replace('-', "");
    if valid_prometheus_name(&instance) {
        instance
    } else {
        String::new()
    }
}

/// Default values supplied by the CLI flags (`--metrics-instance`,
/// `--metrics-comment`, `--push-metrics`), like Go's `instanceFlag`,
/// `commentFlag` and `pushFlag`. The config file remains stronger.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct MetricsCliDefaults {
    pub instance: String,
    pub comment: String,
    pub push: String,
}

/// Registers the three configuration keys, like Go `prepConfig`, with empty
/// CLI defaults. Use [`register_options_with`] to pass flag values.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    register_options_with(config, &MetricsCliDefaults::default())
}

/// Registers the three configuration keys with the given CLI flag defaults.
pub fn register_options_with(
    config: &Config,
    defaults: &MetricsCliDefaults,
) -> pm_config::Result<()> {
    config.register(
        ConfigOption::new(
            "Metrics Instance Name",
            CFG_OPTION_INSTANCE_KEY,
            "Define the prometheus instance label for all exported metrics. Please note that changing the metrics instance name will reset persisted metrics.",
            OPT_TYPE_STRING,
            Value::String(defaults.instance.clone()),
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_requires_restart(true)
        .with_validation_regex("^([a-zA-Z_][a-zA-Z0-9_]*)?$")
        .with_annotation(DISPLAY_ORDER_ANNOTATION, json!(0))
        .with_annotation(CATEGORY_ANNOTATION, json!("Metrics")),
    )?;

    config.register(
        ConfigOption::new(
            "Metrics Comment Label",
            CFG_OPTION_COMMENT_KEY,
            "Define a metrics comment label, which is added to the info metric.",
            OPT_TYPE_STRING,
            Value::String(defaults.comment.clone()),
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_requires_restart(true)
        .with_annotation(DISPLAY_ORDER_ANNOTATION, json!(0))
        .with_annotation(CATEGORY_ANNOTATION, json!("Metrics")),
    )?;

    config.register(
        ConfigOption::new(
            "Push Prometheus Metrics",
            CFG_OPTION_PUSH_KEY,
            "Push metrics to this URL in the prometheus format.",
            OPT_TYPE_STRING,
            Value::String(defaults.push.clone()),
        )
        .with_expertise_level(EXPERTISE_LEVEL_EXPERT)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_requires_restart(true)
        .with_annotation(DISPLAY_ORDER_ANNOTATION, json!(0))
        .with_annotation(CATEGORY_ANNOTATION, json!("Metrics")),
    )?;

    Ok(())
}

/// Reads the configured values after the config file has been loaded.
pub struct ConfigValues {
    pub instance: String,
    pub comment: String,
    pub push: String,
}

impl ConfigValues {
    pub fn read(config: &Arc<Config>) -> ConfigValues {
        let default_instance = default_instance();
        ConfigValues {
            instance: config.get_as_string(CFG_OPTION_INSTANCE_KEY, &default_instance)(),
            comment: config.get_as_string(CFG_OPTION_COMMENT_KEY, "")(),
            push: config.get_as_string(CFG_OPTION_PUSH_KEY, "")(),
        }
    }
}
