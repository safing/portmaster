//! Notification events for network environment changes, port of the
//! notification parts of Go `service/netenv/online-status.go`.
//!
//! # Deviation from Go
//!
//! Go calls the global `notifications.Notify` directly from `netenv`. In Rust
//! the notification manager is owned by `pm-core`, which this crate must not
//! depend on (and must not edit). Instead, [`NotificationSink`] is injected:
//! the caller implements it, e.g. by delegating to
//! `pm_notifications::Notifications::notify`, and receives events whose IDs,
//! titles, messages, types and actions are byte-equal to Go. A no-op sink is
//! the default, so notifications are never silently dropped without an owner.
//!
//! The event structs themselves have the same JSON field names as
//! `notifications.Notification`, so converting them is a straightforward
//! `serde_json::to_value` plus a GUID assignment.

use serde::{Deserialize, Serialize};
use serde_json::json;

use crate::{CaptivePortal, OnlineStatus};

/// Event ID of the captive portal notification.
pub const CAPTIVE_PORTAL_EVENT_ID: &str = "netenv:captive-portal";
/// Event ID of the offline notification.
pub const OFFLINE_EVENT_ID: &str = "netenv:online-status:offline";
/// Event ID of the limited-connectivity notification.
pub const LIMITED_EVENT_ID: &str = "netenv:online-status:limited";

/// Captive portal notification title, like Go.
pub const CAPTIVE_PORTAL_TITLE: &str = "Captive Portal Detected";
/// Captive portal notification message, like Go.
pub const CAPTIVE_PORTAL_MESSAGE: &str =
    "The Portmaster detected a captive portal. You might experience limited network connectivity until the portal is handled.";
/// Offline notification title, like Go.
pub const OFFLINE_TITLE: &str = "Device is Offline";
/// Offline notification message, like Go.
pub const OFFLINE_MESSAGE: &str = "Portmaster did not detect any network connectivity.";
/// Limited-connectivity notification title, like Go.
pub const LIMITED_TITLE: &str = "Limited network connectivity.";
/// Limited-connectivity notification message, like Go.
pub const LIMITED_MESSAGE: &str =
    "Portmaster did detect local network connectivity, but could not detect connectivity to the Internet.";

/// Action ID of the "Ignore" action, like Go.
pub const IGNORE_ACTION_ID: &str = "ack";
/// Text of the "Ignore" action, like Go.
pub const IGNORE_ACTION_TEXT: &str = "Ignore";
/// Text of the "Open Portal" action, like Go.
pub const OPEN_PORTAL_ACTION_TEXT: &str = "Open Portal";
/// Type of the "Open Portal" action, like Go `notifications.ActionTypeOpenURL`.
pub const OPEN_PORTAL_ACTION_TYPE: &str = "open-url";

/// Notification type: informational, like Go `notifications.Info`.
pub const NOTIFICATION_TYPE_INFO: u8 = 0;

/// An action of a notification event, shaped like Go `notifications.Action`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NotificationAction {
    #[serde(rename = "ID")]
    pub id: String,
    #[serde(rename = "Text")]
    pub text: String,
    #[serde(rename = "Type")]
    pub type_: String,
    #[serde(rename = "Payload")]
    pub payload: serde_json::Value,
}

impl NotificationAction {
    /// Creates a plain action with the given ID and text.
    pub fn new(id: &str, text: &str) -> NotificationAction {
        NotificationAction {
            id: id.to_string(),
            text: text.to_string(),
            type_: String::new(),
            payload: serde_json::Value::Null,
        }
    }

    /// Creates the "Open Portal" action, like Go's `ActionTypeOpenURL`.
    pub fn open_url(url: &str) -> NotificationAction {
        NotificationAction {
            id: String::new(),
            text: OPEN_PORTAL_ACTION_TEXT.to_string(),
            type_: OPEN_PORTAL_ACTION_TYPE.to_string(),
            payload: json!(url),
        }
    }
}

/// A notification to be sent by the caller, shaped like Go
/// `notifications.Notification`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NotificationEvent {
    #[serde(rename = "EventID")]
    pub event_id: String,
    #[serde(rename = "Type")]
    pub type_: u8,
    #[serde(rename = "Title")]
    pub title: String,
    #[serde(rename = "Message")]
    pub message: String,
    #[serde(rename = "ShowOnSystem")]
    pub show_on_system: bool,
    #[serde(rename = "EventData")]
    pub event_data: serde_json::Value,
    #[serde(rename = "AvailableActions")]
    pub available_actions: Vec<NotificationAction>,
}

impl NotificationEvent {
    fn info(event_id: &str, title: &str, message: &str) -> NotificationEvent {
        NotificationEvent {
            event_id: event_id.to_string(),
            type_: NOTIFICATION_TYPE_INFO,
            title: title.to_string(),
            message: message.to_string(),
            show_on_system: false,
            event_data: serde_json::Value::Null,
            available_actions: Vec::new(),
        }
    }
}

/// Builds the captive portal notification, like Go `setCaptivePortal`.
pub fn captive_portal_event(portal: &CaptivePortal) -> NotificationEvent {
    NotificationEvent {
        show_on_system: true,
        event_data: serde_json::to_value(portal).unwrap_or(serde_json::Value::Null),
        available_actions: vec![
            NotificationAction::open_url(&portal.url),
            NotificationAction::new(IGNORE_ACTION_ID, IGNORE_ACTION_TEXT),
        ],
        ..NotificationEvent::info(
            CAPTIVE_PORTAL_EVENT_ID,
            CAPTIVE_PORTAL_TITLE,
            CAPTIVE_PORTAL_MESSAGE,
        )
    }
}

/// Builds the offline/limited notification, like Go `notifyOnlineStatus`.
///
/// Returns `None` for all other statuses, where Go deletes an existing
/// notification instead. [`NotificationEvent::None`] is reported separately by
/// [`notification_change`] so the caller can delete the previous notification.
pub fn online_status_event(status: OnlineStatus) -> Option<NotificationEvent> {
    match status {
        OnlineStatus::Offline => Some(NotificationEvent::info(
            OFFLINE_EVENT_ID,
            OFFLINE_TITLE,
            OFFLINE_MESSAGE,
        )),
        OnlineStatus::Limited => Some(NotificationEvent::info(
            LIMITED_EVENT_ID,
            LIMITED_TITLE,
            LIMITED_MESSAGE,
        )),
        _ => None,
    }
}

/// Changes to the notifications belonging to the network environment, like
/// Go calling `Notify` or `Notification.Delete`.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct NotificationChange {
    /// Events to send; replaces any previous online status notification.
    pub notify: Vec<NotificationEvent>,
    /// Delete the previous online status notification, like Go's `Delete`.
    pub delete_online_status: bool,
    /// Delete the previous captive portal notification, like Go's `Delete`.
    pub delete_captive_portal: bool,
}

/// Computes the notification changes of an online status update, like Go's
/// `notifyOnlineStatus` and `setCaptivePortal` taken together.
///
/// `had_portal` and `had_status_notification` describe the previously active
/// notifications. The caller owns those handles in Go; here the caller passes
/// the state in and applies the returned changes.
pub fn notification_change(
    status: OnlineStatus,
    portal: Option<&CaptivePortal>,
    had_portal: bool,
    active_status_event_id: Option<&str>,
) -> NotificationChange {
    let mut change = NotificationChange::default();

    match online_status_event(status) {
        Some(event) => {
            if active_status_event_id.is_some_and(|id| id != event.event_id) {
                // Go deletes the old notification when Offline and Limited
                // switch, because their event IDs differ.
                change.delete_online_status = true;
            }
            if active_status_event_id != Some(event.event_id.as_str()) {
                change.notify.push(event);
            }
        }
        None => change.delete_online_status = active_status_event_id.is_some(),
    }

    match portal {
        Some(portal) if !had_portal => change.notify.push(captive_portal_event(portal)),
        Some(_) => {}
        None => change.delete_captive_portal = had_portal,
    }

    change
}

/// Sink for notification changes, injected by the caller.
pub trait NotificationSink: Send + Sync {
    /// Handles a notification change.
    fn handle(&self, change: NotificationChange);
}

/// Sink that discards all events, used when no owner is wired up.
#[derive(Debug, Default, Clone, Copy)]
pub struct NoopNotificationSink;

impl NotificationSink for NoopNotificationSink {
    fn handle(&self, _change: NotificationChange) {}
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn portal_event_matches_go() {
        let portal = CaptivePortal::new("http://portal.example/login", "portal.example", None);
        let event = captive_portal_event(&portal);
        assert_eq!(event.event_id, CAPTIVE_PORTAL_EVENT_ID);
        assert!(event.show_on_system);
        assert_eq!(event.type_, NOTIFICATION_TYPE_INFO);
        assert_eq!(event.title, CAPTIVE_PORTAL_TITLE);
        assert_eq!(event.available_actions.len(), 2);
        assert_eq!(event.available_actions[0].type_, "open-url");
        assert_eq!(event.available_actions[0].payload, json!(portal.url));
        assert_eq!(event.available_actions[1].id, IGNORE_ACTION_ID);
        assert_eq!(event.available_actions[1].text, IGNORE_ACTION_TEXT);
        assert_eq!(event.event_data, serde_json::to_value(&portal).unwrap());
    }

    #[test]
    fn status_events_only_for_offline_and_limited() {
        assert_eq!(
            online_status_event(OnlineStatus::Offline).unwrap().event_id,
            OFFLINE_EVENT_ID
        );
        assert_eq!(
            online_status_event(OnlineStatus::Limited).unwrap().event_id,
            LIMITED_EVENT_ID
        );
        assert!(online_status_event(OnlineStatus::Online).is_none());
        assert!(online_status_event(OnlineStatus::Portal).is_none());
    }
}
