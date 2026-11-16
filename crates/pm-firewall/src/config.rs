//! Configuration options of the privacy filter, port of
//! `service/firewall/config.go`.

use serde_json::{json, Value};

use pm_config::{Config, ConfigOption};

/// Whether the privacy filter is enabled at all.
pub const CFG_OPTION_ENABLE_FILTER_KEY: &str = "filter/enable";
/// Whether verdicts are made permanent in the system integration.
pub const CFG_OPTION_PERMANENT_VERDICTS_KEY: &str = "filter/permanentVerdicts";
/// Whether astray DNS queries are redirected to the internal DNS server.
pub const CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY: &str = "filter/dnsQueryInterception";
/// Whether prompt notifications are also sent to the desktop.
pub const CFG_OPTION_ASK_WITH_SYSTEM_NOTIFICATIONS_KEY: &str = "filter/askWithSystemNotifications";
/// How long a prompt waits for an answer.
pub const CFG_OPTION_ASK_TIMEOUT_KEY: &str = "filter/askTimeout";

/// Display order of the system notification prompt option.
const ASK_WITH_SYSTEM_NOTIFICATIONS_ORDER: i64 = 2;
/// Display order of the prompt timeout option.
const ASK_TIMEOUT_ORDER: i64 = 3;
/// Display order of the permanent verdicts option.
const PERMANENT_VERDICTS_ORDER: i64 = 80;
/// Display order of the DNS query interception option.
const DNS_QUERY_INTERCEPTION_ORDER: i64 = 81;

/// Registers the firewall options, in the same order as Go.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    config.register(
        ConfigOption::new(
            "Enable Privacy Filter",
            CFG_OPTION_ENABLE_FILTER_KEY,
            "Enable the Privacy Filter. If turned off, all privacy filter protections are fully disabled on this device. Not meant to be disabled in production - only turn off for testing.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_release_level(pm_config::RELEASE_LEVEL_EXPERIMENTAL)
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General")),
    )?;

    config.register(
        ConfigOption::new(
            "Permanent Verdicts",
            CFG_OPTION_PERMANENT_VERDICTS_KEY,
            "The Portmaster's system integration intercepts every single packet. Usually the first packet is enough for the Portmaster to set the verdict for a connection - ie. to allow or deny it. Making these verdicts permanent means that the Portmaster will tell the system integration that is does not want to see any more packets of this single connection. This brings a major performance increase.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_release_level(pm_config::RELEASE_LEVEL_EXPERIMENTAL)
        .with_annotation(
            pm_config::DISPLAY_ORDER_ANNOTATION,
            json!(PERMANENT_VERDICTS_ORDER),
        )
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Advanced")),
    )?;

    config.register(
        ConfigOption::new(
            "Seamless DNS Integration",
            CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY,
            "Intercept and redirect astray DNS queries to the Portmaster's internal DNS server. This enables seamless DNS integration without having to configure the system or other software. However, this may lead to compatibility issues with other software that attempts the same.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_release_level(pm_config::RELEASE_LEVEL_EXPERIMENTAL)
        .with_annotation(
            pm_config::DISPLAY_ORDER_ANNOTATION,
            json!(DNS_QUERY_INTERCEPTION_ORDER),
        )
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Advanced")),
    )?;

    config.register(
        ConfigOption::new(
            "Prompt Desktop Notifications",
            CFG_OPTION_ASK_WITH_SYSTEM_NOTIFICATIONS_KEY,
            "In addition to showing prompt notifications in the Portmaster App, also send them to the Desktop. This requires the Portmaster Notifier to be running. Requires Desktop Notifications to be enabled.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_USER)
        .with_annotation(
            pm_config::DISPLAY_ORDER_ANNOTATION,
            json!(ASK_WITH_SYSTEM_NOTIFICATIONS_ORDER),
        )
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General"))
        .with_annotation(
            pm_config::REQUIRES_ANNOTATION,
            json!({
                "Key": pm_notifications::CFG_USE_SYSTEM_NOTIFICATIONS_KEY,
                "Value": true,
            }),
        ),
    )?;

    config.register(
        ConfigOption::new(
            "Prompt Timeout",
            CFG_OPTION_ASK_TIMEOUT_KEY,
            "How long the Portmaster will wait for a reply to a prompt notification. Please note that Desktop Notifications might not respect this or have their own limits.",
            pm_config::OPT_TYPE_INT,
            Value::from(60),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_USER)
        .with_validation_regex("^[1-9][0-9]{1,5}$")
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(ASK_TIMEOUT_ORDER))
        .with_annotation(pm_config::UNIT_ANNOTATION, json!("seconds"))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General")),
    )?;

    Ok(())
}
