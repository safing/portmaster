//! Compatible implementation of the Go `base/notifications` package.
//!
//! Notifications have a type, a state and optional actions. They are kept in an
//! in-memory registry, exposed through the injected `notifications` database and
//! can be deleted, expired or answered by the user.
//!
//! # Compatibility notes
//!
//! * `EventData` is a JSON value; Go allows arbitrary values with custom
//!   lockers, which is not needed in Rust.
//! * Action handlers are called synchronously after the state was updated;
//!   waiting for a user response uses `tokio::sync::watch` channels.
//! * New random GUIDs are UUID v4; Go mixes the event ID into the random UUID.
//! * JSON objects inside `EventData` and action payloads keep the order of the
//!   `serde_json` map; Go sorts map keys. Notifications are never persisted, so
//!   only the in-memory representation differs.
//! * The module manager and the periodic cleaner task live in the core; the
//!   crate provides `clean_expired()` for that timer.

mod config;
mod registry;
mod storage;

use serde::{Deserialize, Serialize};
use serde_json::Value;

pub use config::{register_options, CFG_USE_SYSTEM_NOTIFICATIONS_KEY};
pub use registry::{
    ActionHandler, Error, NotificationHandle, Notifications, Result, NOTIFICATIONS_DB_PATH,
};
pub use storage::NotificationsStorage;

/// Type of a notification.
pub type NotificationType = u8;
/// Informational notification.
pub const TYPE_INFO: NotificationType = 0;
/// Warning notification.
pub const TYPE_WARNING: NotificationType = 1;
/// Prompt notification.
pub const TYPE_PROMPT: NotificationType = 2;
/// Error notification.
pub const TYPE_ERROR: NotificationType = 3;

/// State of a notification.
pub type NotificationState = String;
/// The notification is active and, if it has actions, waits for the user.
pub const STATE_ACTIVE: &str = "active";
/// The user responded, but the action is still running.
pub const STATE_RESPONDED: &str = "responded";
/// The selected action was executed.
pub const STATE_EXECUTED: &str = "executed";

/// Visibility of an action.
pub type ActionVisibility = String;
/// The action is always visible.
pub const ACTION_VISIBILITY_DEFAULT: &str = "";
/// The action is only visible in the detailed view.
pub const ACTION_VISIBILITY_DETAILED: &str = "detailed";
/// The action is only visible in the app.
pub const ACTION_VISIBILITY_IN_APP_ONLY: &str = "in-app-only";

/// Type of an action.
pub type ActionType = String;
/// Report the selected ID back to the backend.
pub const ACTION_TYPE_NONE: &str = "";
/// Open an external URL.
pub const ACTION_TYPE_OPEN_URL: &str = "open-url";
/// Open a page.
pub const ACTION_TYPE_OPEN_PAGE: &str = "open-page";
/// Open a setting.
pub const ACTION_TYPE_OPEN_SETTING: &str = "open-setting";
/// Open a profile.
pub const ACTION_TYPE_OPEN_PROFILE: &str = "open-profile";
/// Inject an event.
pub const ACTION_TYPE_INJECT_EVENT: &str = "inject-event";
/// Call a webhook.
pub const ACTION_TYPE_WEBHOOK: &str = "call-webhook";

/// Payload of the `open-setting` action type.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ActionTypeOpenSettingPayload {
    #[serde(rename = "Key")]
    pub key: String,
    #[serde(rename = "Profile")]
    pub profile: String,
}

/// Payload of the `call-webhook` action type.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ActionTypeWebhookPayload {
    #[serde(rename = "Method")]
    pub method: String,
    #[serde(rename = "URL")]
    pub url: String,
    #[serde(rename = "Payload")]
    pub payload: Value,
    #[serde(rename = "ResultAction")]
    pub result_action: String,
}

/// An action that can be taken for a notification.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Action {
    #[serde(rename = "ID")]
    pub id: String,
    #[serde(rename = "Text")]
    pub text: String,
    #[serde(rename = "Type")]
    pub type_: ActionType,
    #[serde(rename = "Payload")]
    pub payload: Value,
    #[serde(rename = "Visibility")]
    pub visibility: ActionVisibility,
}

impl Action {
    /// Creates an action with the given ID and text.
    pub fn new(id: &str, text: &str) -> Action {
        Action {
            id: id.to_string(),
            text: text.to_string(),
            ..Action::default()
        }
    }

    /// Sets the action type and payload.
    #[must_use]
    pub fn with_type(mut self, type_: &str, payload: Value) -> Action {
        self.type_ = type_.to_string();
        self.payload = payload;
        self
    }

    /// Sets the visibility.
    #[must_use]
    pub fn with_visibility(mut self, visibility: &str) -> Action {
        self.visibility = visibility.to_string();
        self
    }
}

/// A notification for the user.
///
/// Field names and order match the Go struct.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Notification {
    #[serde(rename = "EventID")]
    pub event_id: String,
    #[serde(rename = "GUID")]
    pub guid: String,
    #[serde(rename = "Type")]
    pub type_: NotificationType,
    #[serde(rename = "Title")]
    pub title: String,
    #[serde(rename = "Category")]
    pub category: String,
    #[serde(rename = "Message")]
    pub message: String,
    #[serde(rename = "ShowOnSystem")]
    pub show_on_system: bool,
    #[serde(rename = "EventData")]
    pub event_data: Value,
    #[serde(rename = "Expires")]
    pub expires: i64,
    #[serde(rename = "State")]
    pub state: NotificationState,
    #[serde(rename = "AvailableActions")]
    pub available_actions: Vec<Action>,
    #[serde(rename = "SelectedActionID")]
    pub selected_action_id: String,
}

impl Default for Notification {
    fn default() -> Self {
        Notification {
            event_id: String::new(),
            guid: String::new(),
            type_: TYPE_INFO,
            title: String::new(),
            category: String::new(),
            message: String::new(),
            show_on_system: false,
            event_data: Value::Null,
            expires: 0,
            state: String::new(),
            available_actions: Vec::new(),
            selected_action_id: String::new(),
        }
    }
}

impl Notification {
    /// Creates an info notification with an acknowledgement action.
    pub fn info(id: &str, title: &str, message: &str) -> Notification {
        Notification::new(TYPE_INFO, id, title, message, false)
    }

    /// Creates a warning notification.
    pub fn warning(id: &str, title: &str, message: &str) -> Notification {
        Notification::new(TYPE_WARNING, id, title, message, true)
    }

    /// Creates an error notification.
    pub fn error(id: &str, title: &str, message: &str) -> Notification {
        Notification::new(TYPE_ERROR, id, title, message, true)
    }

    /// Creates a prompt notification.
    pub fn prompt(id: &str, title: &str, message: &str) -> Notification {
        Notification::new(TYPE_PROMPT, id, title, message, false)
    }

    /// Creates a notification with the given type and no actions yet.
    pub fn new(
        type_: NotificationType,
        id: &str,
        title: &str,
        message: &str,
        show_on_system: bool,
    ) -> Notification {
        Notification {
            event_id: id.to_string(),
            type_,
            title: title.to_string(),
            message: message.to_string(),
            show_on_system,
            ..Notification::default()
        }
    }

    /// Adds an action; without any actions a default acknowledgement is added on
    /// save.
    #[must_use]
    pub fn with_action(mut self, action: Action) -> Notification {
        self.available_actions.push(action);
        self
    }

    /// Sets the event data.
    #[must_use]
    pub fn with_event_data(mut self, data: Value) -> Notification {
        self.event_data = data;
        self
    }

    /// Sets the expiry timestamp.
    #[must_use]
    pub fn with_expiry(mut self, expires: i64) -> Notification {
        self.expires = expires;
        self
    }

    /// Returns the database key of the notification.
    pub fn key(&self) -> String {
        format!("notifications:all/{}", self.event_id)
    }

    /// Returns whether the notification is active.
    pub fn is_active(&self) -> bool {
        self.state == STATE_ACTIVE
    }

    /// Returns whether the notification has expired at the given time.
    pub fn is_expired(&self, now: i64) -> bool {
        self.expires > 0 && self.expires < now
    }

    /// Returns the action with the given ID.
    pub fn action(&self, id: &str) -> Option<&Action> {
        self.available_actions.iter().find(|action| action.id == id)
    }
}
