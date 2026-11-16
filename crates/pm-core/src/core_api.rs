//! Remaining core API endpoints from Go `service/core/api.go`.
//!
//! `app/profile` needs the process identity of the calling API client, which
//! `pm-api` does not carry in its auth token yet; the endpoint is registered
//! and reports that limitation instead of returning a wrong profile.
//!
//! `app/auth` follows the Go flow: validate the requested permissions, show a
//! prompt, generate a random API key on "Authorize" and append it to
//! `core/apiKeys`. The waiting is done on a blocking API worker, like Go.

use std::sync::mpsc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use pm_api::{Api, ApiError, Endpoint, Result, PERMIT_ANYONE, PERMIT_USER};
use pm_config::Config;
use pm_notifications::{Action, Notification, Notifications};
use serde_json::json;

use crate::service::CFG_API_KEYS;

/// Registers `app/profile`, `app/auth` and `debug/core`.
pub fn register_api(
    api: &Api,
    config: std::sync::Arc<Config>,
    notifications: std::sync::Arc<Notifications>,
    netquery_db: std::sync::Arc<pm_netquery::Database>,
    profiles: std::sync::Arc<crate::firewall::CoreProfiles>,
) -> Result<()> {
    api.register_endpoint(
        Endpoint::new(
            "app/profile",
            "Get the ID of the calling profile",
            "",
        )
        .with_read(PERMIT_USER)
        .with_struct(move |request| {
            let caller = request.auth().and_then(|token| token.process.clone())
                .ok_or_else(|| ApiError::Message(
                    "failed to identify requesting process: the API token does not carry process identity".to_string(),
                ).with_status(500))?;
            let cache = pm_process::ProcessCache::with_default_tag_handlers(
                pm_process::ProcReader::new(pm_netstate::ProcFs::default()),
            );
            let process = cache.get_or_find(caller.pid).map_err(|error| {
                ApiError::Message(format!("failed to identify requesting process: {error}"))
                    .with_status(500)
            })?;
            let layered = profiles.layered_for(&process).ok_or_else(|| {
                ApiError::Message("unknown process or profile".to_string()).with_status(500)
            })?;
            let local = layered.local_profile();
            Ok(json!({
                "profile": local.id,
                "source": local.source,
                "name": local.name,
            }))
        }),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "app/auth",
            "Request an authentication token with a given set of permissions. The user will be prompted to either authorize or deny the request. Used for external or third-party tool integrations.",
            "",
        )
        .with_read(PERMIT_ANYONE)
        .with_struct(move |request| {
            auth_app(
                &config,
                &notifications,
                request.query_param("app-name").unwrap_or_default(),
                request.query_param("read").unwrap_or_default(),
                request.query_param("write").unwrap_or_default(),
                request.query_param("ttl").unwrap_or_default(),
            )
        }),
    )?;

    api.register_endpoint(
        Endpoint::new(
            "debug/core",
            "Get Debug Information",
            "Returns network debugging information, similar to debug/info, but with system status data.",
        )
        .with_read(PERMIT_ANYONE)
        .with_data(move |request| {
            let style = request.query_param("style").unwrap_or_default();
            Ok(debug_info(&netquery_db, style).into_bytes())
        }),
    )?;

    Ok(())
}

fn auth_app(
    config: &std::sync::Arc<Config>,
    notifications: &Notifications,
    app_name: &str,
    read: &str,
    write: &str,
    ttl: &str,
) -> Result<serde_json::Value> {
    let read_permission = pm_api::parse_permission(read).filter(|permission| {
        *permission == PERMIT_USER || *permission == pm_api::PERMIT_ADMIN
    });
    let write_permission = pm_api::parse_permission(write).filter(|permission| {
        *permission == PERMIT_USER || *permission == pm_api::PERMIT_ADMIN
    });
    let (Some(_read_permission), Some(_write_permission)) = (read_permission, write_permission) else {
        return Err(ApiError::Message("invalid read permission".to_string()));
    };

    let ttl_seconds = if ttl.is_empty() {
        24 * 60 * 60
    } else {
        parse_duration_seconds(ttl).ok_or_else(|| ApiError::Message("invalid ttl".to_string()))?
    };
    let valid_until = now() + ttl_seconds;
    let event_id = format!("core:authorize-app-{}", now());

    let mut notification = Notification::prompt(
        &event_id,
        "An app requests access to the Portmaster",
        &format!(
            "Allow {app_name} to query and modify the Portmaster?\n\nRequested read: {read}, write: {write}."
        ),
    )
    .with_action(Action::new("allow", "Authorize"))
    .with_action(Action::new("deny", "Deny"))
    .with_expiry(now() + 60);
    notification.show_on_system = true;

    let (tx, rx) = mpsc::channel::<String>();
    let config_for_action = config.clone();
    let read_text = read.to_string();
    let write_text = write.to_string();
    let handle = notifications
        .notify(notification)
        .ok_or_else(|| ApiError::Message("failed to create prompt".to_string()))?;
    handle.set_action_handler(std::sync::Arc::new(move |notification| {
        if notification.selected_action_id != "allow" {
            return Ok(());
        }
        let mut bytes = [0u8; 8];
        getrandom::fill(&mut bytes).map_err(|error| error.to_string())?;
        let key = hex::encode(bytes);
        let mut keys = config_for_action.get_as_string_array(CFG_API_KEYS, &[])();
        keys.push(format!(
            "{key}?read={}&write={}&expires={}",
            read_text, write_text,
            format_rfc3339(valid_until)
        ));
        config_for_action
            .set_config_option(CFG_API_KEYS, serde_json::json!(keys))
            .map_err(|error| error.to_string())?;
        tx.send(key).map_err(|error| error.to_string())?;
        Ok(())
    }));

    match rx.recv_timeout(Duration::from_secs(60)) {
        Ok(key) => Ok(json!({
            "key": key,
            "validUntil": format_rfc3339(valid_until),
        })),
        Err(mpsc::RecvTimeoutError::Disconnected) => {
            Err(ApiError::Message("access denied".to_string()))
        }
        Err(mpsc::RecvTimeoutError::Timeout) => Err(ApiError::Message("timeout".to_string())),
    }
}

/// Parses Go durations with an hour/day suffix, or a plain number of seconds.
fn parse_duration_seconds(value: &str) -> Option<i64> {
    if let Ok(seconds) = value.parse::<i64>() {
        return Some(seconds);
    }
    let (number, multiplier) = if let Some(number) = value.strip_suffix("ms") {
        (number, 0.001)
    } else if let Some(number) = value.strip_suffix('s') {
        (number, 1.0)
    } else if let Some(number) = value.strip_suffix('m') {
        (number, 60.0)
    } else if let Some(number) = value.strip_suffix('h') {
        (number, 3600.0)
    } else {
        return None;
    };
    let number = number.parse::<f64>().ok()?;
    Some((number * multiplier) as i64)
}

fn format_rfc3339(seconds: i64) -> String {
    pm_dsd::gotime::format_rfc3339(seconds)
}

fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs() as i64)
        .unwrap_or(0)
}

/// Builds the debug output, like Go `service/core/api.go::debugInfo`.
///
/// Sections use Go's `base/utils/debug` layout via `pm_debug`. Go-specific
/// data that has no Rust equivalent (goroutine stacks, worker info, module
/// states) is represented by the sections that are available here.
fn debug_info(db: &pm_netquery::Database, style: &str) -> String {
    use pm_debug::{ADD_CONTENT_LINE_BREAKS, USE_CODE_SECTION};

    let mut di = pm_debug::Info::new().with_style(style);
    let version = crate::cli::version();
    di.add_version_info(&version, &format!("Portmaster Core (Rust)\n{version}\n"));

    // Go uses gopsutil host.Info here; read the equivalent Linux sources.
    let (platform, platform_version, pretty_name) = os_release();
    let kernel = std::fs::read_to_string("/proc/sys/kernel/osrelease")
        .unwrap_or_default()
        .trim()
        .to_string();
    let system = if pretty_name.is_empty() {
        platform.clone()
    } else {
        pretty_name
    };
    let family = platform_family(&platform);
    let section_name = format!("Platform: {platform} {platform_version}");
    let platform_lines = [
        format!("System: {system} {} ({family}) {platform_version}", std::env::consts::OS),
        format!("Kernel: {kernel} {}", std::env::consts::ARCH),
    ];
    di.add_section(
        &section_name,
        USE_CODE_SECTION | ADD_CONTENT_LINE_BREAKS,
        &platform_lines.iter().map(String::as_str).collect::<Vec<_>>(),
    );

    // The Rust core uses tracing without an in-memory log ring yet; Go prints
    // this section when no unexpected logs were recorded.
    di.add_last_unexpected_logs(&[], "");

    let netenv = pm_netenv::shared();
    let status = netenv.online_status().to_string();
    let portal = netenv
        .captive_portal()
        .map(|portal| portal.url)
        .unwrap_or_default();
    let status_lines = [
        format!("OnlineStatus:          {status}"),
        format!("CaptivePortal:         {portal}"),
    ];
    di.add_section(
        &format!("Status: {status}"),
        USE_CODE_SECTION | ADD_CONTENT_LINE_BREAKS,
        &status_lines.iter().map(String::as_str).collect::<Vec<_>>(),
    );

    let connections = match db.count_rows() {
        Ok(count) => format!("Connections: {count}"),
        Err(error) => format!("Connections: unavailable ({error})"),
    };
    match pm_netenv::network().addresses.get_assigned_addresses() {
        Ok((ipv4, ipv6)) => di.add_section(
            "Network Addresses",
            USE_CODE_SECTION | ADD_CONTENT_LINE_BREAKS,
            &[
                &connections,
                &format!("IPv4 addresses: {}", ipv4.len()),
                &format!("IPv6 addresses: {}", ipv6.len()),
            ],
        ),
        Err(_) => di.add_section("Network Addresses", USE_CODE_SECTION, &[&connections]),
    }

    di.into_string()
}

/// Returns (ID, VERSION_ID, PRETTY_NAME) from `/etc/os-release`.
fn os_release() -> (String, String, String) {
    let Ok(contents) = std::fs::read_to_string("/etc/os-release") else {
        return ("unknown".into(), String::new(), String::new());
    };
    let mut values = std::collections::HashMap::new();
    for line in contents.lines() {
        if let Some((key, value)) = line.split_once('=') {
            values.insert(
                key,
                value.trim().trim_matches('"').replace("\\\"", "\""),
            );
        }
    }
    (
        values.get("ID").cloned().unwrap_or_else(|| "unknown".into()),
        values.get("VERSION_ID").cloned().unwrap_or_default(),
        values.get("PRETTY_NAME").cloned().unwrap_or_default(),
    )
}

/// Rough equivalent of gopsutil's `PlatformFamily`.
fn platform_family(platform: &str) -> &'static str {
    match platform {
        "ubuntu" | "debian" | "linuxmint" | "raspbian" => "debian",
        "fedora" | "rhel" | "centos" | "rocky" | "almalinux" => "rhel",
        "arch" | "manjaro" => "arch",
        "opensuse" | "sles" => "suse",
        "alpine" => "alpine",
        _ => "unknown",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_go_durations() {
        assert_eq!(parse_duration_seconds("60"), Some(60));
        assert_eq!(parse_duration_seconds("5s"), Some(5));
        assert_eq!(parse_duration_seconds("24h"), Some(86400));
        assert_eq!(parse_duration_seconds("2m"), Some(120));
        assert_eq!(parse_duration_seconds("1500ms"), Some(1));
        assert_eq!(parse_duration_seconds("nope"), None);
    }
}
