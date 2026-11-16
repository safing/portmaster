//! YAML source parsing, ported from Go `service/broadcasts/notify.go`.
//!
//! Go uses `ghodss/yaml`, which converts YAML to JSON and then uses
//! `encoding/json` to fill the structs. `encoding/json` matches field names
//! case-insensitively and ignores unknown keys; the aliases below reproduce
//! that behaviour for the keys used by the real `notifications.yaml` files.

use std::collections::BTreeMap;
use std::time::Duration;

use pm_notifications::{Action, Notification};
use pm_query::{parse_query, Query};
use serde::Deserialize;
use serde_json::Value;

use crate::{Error, Result};

/// Name of the intel resource that holds the broadcast definitions.
pub const BROADCASTS_RESOURCE_NAME: &str = "notifications.yaml";
/// Prefix of the event ID of a broadcast notification.
pub const BROADCAST_NOTIFICATION_ID_PREFIX: &str = "broadcasts:";
/// Minimum repeat duration, like Go `minRepeatDuration`.
pub const MIN_REPEAT_DURATION: Duration = Duration::from_secs(60 * 60);

/// The parsed broadcast notification update file.
#[derive(Debug, Default)]
pub struct BroadcastNotifications {
    pub notifications: BTreeMap<String, BroadcastNotification>,
}

/// A single broadcast notification.
#[derive(Debug, Clone)]
pub struct BroadcastNotification {
    /// The key under `notifications:` in the YAML file.
    pub id: String,
    /// The embedded `notifications.Notification`.
    pub notification: Notification,
    pub match_: String,
    pub matching_query: Option<Query>,
    pub attach_to_module: bool,
    pub remove: bool,
    pub permanent: bool,
    pub repeat: String,
    pub repeat_duration: Duration,
}

#[derive(Debug, Default, Deserialize)]
struct BroadcastNotificationsYaml {
    #[serde(rename = "notifications", alias = "Notifications", default)]
    notifications: BTreeMap<String, BroadcastNotificationYaml>,
}

#[derive(Debug, Default, Deserialize)]
#[allow(clippy::struct_excessive_bools)] // mirrors the Go struct field-for-field
struct BroadcastNotificationYaml {
    // Fields of the embedded *notifications.Notification.
    #[serde(rename = "EventID", alias = "eventID", alias = "eventid", default)]
    event_id: String,
    #[serde(rename = "GUID", alias = "guid", alias = "Guid", default)]
    guid: String,
    #[serde(rename = "Type", alias = "type", default)]
    type_: u8,
    #[serde(rename = "Title", alias = "title", default)]
    title: String,
    #[serde(rename = "Category", alias = "category", default)]
    category: String,
    #[serde(rename = "Message", alias = "message", default)]
    message: String,
    #[serde(rename = "ShowOnSystem", alias = "showOnSystem", alias = "showonsystem", default)]
    show_on_system: bool,
    #[serde(rename = "EventData", alias = "eventData", alias = "eventdata", default)]
    event_data: Value,
    #[serde(rename = "Expires", alias = "expires", default)]
    expires: i64,
    #[serde(rename = "State", alias = "state", default)]
    state: String,
    #[serde(
        rename = "AvailableActions",
        alias = "availableActions",
        alias = "availableactions",
        default
    )]
    available_actions: Vec<ActionYaml>,
    #[serde(
        rename = "SelectedActionID",
        alias = "selectedActionID",
        alias = "selectedactionid",
        default
    )]
    selected_action_id: String,

    // Broadcast-specific fields.
    #[serde(rename = "Match", alias = "match", default)]
    match_: String,
    #[serde(
        rename = "AttachToModule",
        alias = "attachToModule",
        alias = "attachtomodule",
        default
    )]
    attach_to_module: bool,
    #[serde(rename = "Remove", alias = "remove", default)]
    remove: bool,
    #[serde(rename = "Permanent", alias = "permanent", default)]
    permanent: bool,
    #[serde(rename = "Repeat", alias = "repeat", default)]
    repeat: String,
}

#[derive(Debug, Default, Deserialize)]
struct ActionYaml {
    #[serde(rename = "ID", alias = "id", default)]
    id: String,
    #[serde(rename = "Text", alias = "text", default)]
    text: String,
    #[serde(rename = "Type", alias = "type", default)]
    type_: String,
    #[serde(rename = "Payload", alias = "payload", default)]
    payload: Value,
    #[serde(rename = "Visibility", alias = "visibility", default)]
    visibility: String,
}

impl From<ActionYaml> for Action {
    fn from(action: ActionYaml) -> Action {
        Action {
            id: action.id,
            text: action.text,
            type_: action.type_,
            payload: action.payload,
            visibility: action.visibility,
        }
    }
}

/// Parses a broadcast source, like Go `parseBroadcastSource`.
pub fn parse_broadcast_source(yaml_data: &[u8]) -> Result<BroadcastNotifications> {
    let parsed: BroadcastNotificationsYaml = serde_yaml::from_slice(yaml_data)?;
    let mut broadcasts = BroadcastNotifications::default();

    for (id, bn) in parsed.notifications {
        // Parse the matching query. Go prepends "query / where " to the match
        // expression; the slash is an empty database/key prefix.
        let matching_query = if bn.match_.is_empty() {
            None
        } else {
            let expression = format!("query / where {}", bn.match_);
            Some(parse_query(&expression).map_err(|error| {
                Error::Message(format!(
                    "failed to parse query of broadcast notification {id}: {error}"
                ))
            })?)
        };

        // Parse the repeat duration and raise it to the configured minimum.
        let repeat_duration = if bn.repeat.is_empty() {
            Duration::ZERO
        } else {
            let duration = parse_duration(&bn.repeat).map_err(|error| {
                Error::Message(format!(
                    "failed to parse repeat duration of broadcast notification {id}: {error}"
                ))
            })?;
            if duration < MIN_REPEAT_DURATION {
                MIN_REPEAT_DURATION
            } else {
                duration
            }
        };

        let notification = Notification {
            event_id: bn.event_id,
            guid: bn.guid,
            type_: bn.type_,
            title: bn.title,
            category: bn.category,
            message: bn.message,
            show_on_system: bn.show_on_system,
            event_data: bn.event_data,
            expires: bn.expires,
            state: bn.state,
            available_actions: bn.available_actions.into_iter().map(Into::into).collect(),
            selected_action_id: bn.selected_action_id,
        };

        broadcasts.notifications.insert(
            id.clone(),
            BroadcastNotification {
                id,
                notification,
                match_: bn.match_,
                matching_query,
                attach_to_module: bn.attach_to_module,
                remove: bn.remove,
                permanent: bn.permanent,
                repeat: bn.repeat,
                repeat_duration,
            },
        );
    }

    Ok(broadcasts)
}

/// Parses a Go `time.ParseDuration` string. Supports the standard units
/// (`ns`, `us`/`µs`, `ms`, `s`, `m`, `h`) and decimal values, including `0`.
pub fn parse_duration(input: &str) -> std::result::Result<Duration, String> {
    let invalid = || format!("time: invalid duration \"{input}\"");
    if input == "0" {
        return Ok(Duration::ZERO);
    }
    let bytes = input.as_bytes();
    let mut pos = 0;
    let mut negative = false;
    if pos < bytes.len() && (bytes[pos] == b'-' || bytes[pos] == b'+') {
        negative = bytes[pos] == b'-';
        pos += 1;
    }
    let mut total_ns: f64 = 0.0;
    let mut saw_number = false;
    while pos < bytes.len() {
        let number_start = pos;
        let mut saw_dot = false;
        while pos < bytes.len() {
            let c = bytes[pos];
            if c.is_ascii_digit() {
                pos += 1;
            } else if c == b'.' && !saw_dot {
                saw_dot = true;
                pos += 1;
            } else {
                break;
            }
        }
        if pos == number_start {
            return Err(invalid());
        }
        let number: f64 = input[number_start..pos].parse().map_err(|_| invalid())?;
        saw_number = true;

        let unit_start = pos;
        while pos < bytes.len() && !bytes[pos].is_ascii_digit() && bytes[pos] != b'.' {
            pos += 1;
        }
        let unit = &input[unit_start..pos];
        let factor: f64 = match unit {
            "ns" => 1.0,
            "us" | "µs" | "μs" => 1_000.0,
            "ms" => 1_000_000.0,
            "s" => 1_000_000_000.0,
            "m" => 60.0 * 1_000_000_000.0,
            "h" => 60.0 * 60.0 * 1_000_000_000.0,
            _ => return Err(invalid()),
        };
        total_ns += number * factor;
    }
    if !saw_number {
        return Err(invalid());
    }
    let total_ns = if negative { -total_ns } else { total_ns };
    Ok(Duration::from_nanos(total_ns.max(0.0) as u64))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_go_durations() {
        assert_eq!(parse_duration("1h").unwrap(), Duration::from_secs(3600));
        assert_eq!(parse_duration("1h30m").unwrap(), Duration::from_secs(5400));
        assert_eq!(parse_duration("1.5h").unwrap(), Duration::from_secs(5400));
        assert_eq!(parse_duration("0").unwrap(), Duration::ZERO);
        assert_eq!(parse_duration("30s").unwrap(), Duration::from_secs(30));
        assert!(parse_duration("nope").is_err());
    }

    #[test]
    fn parses_testdata_yaml() {
        let data = include_bytes!("../../../service/broadcasts/testdata/notifications.yaml");
        let parsed = parse_broadcast_source(data).unwrap();
        let bn = &parsed.notifications["new-network-monitor-feedback"];
        assert_eq!(bn.notification.title, "New: Full-Device Network Activity");
        assert!(bn.matching_query.is_some());
    }
}
