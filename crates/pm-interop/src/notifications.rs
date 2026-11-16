//! Notifications of the IVPN interoperability module.
//!
//! Every identifier, title, message, action and database key below is kept
//! byte-for-byte compatible with the Go `service/interop/ivpn/notification.go`.

use std::sync::Arc;

use pm_notifications::{Action, Notification, Notifications, ACTION_VISIBILITY_DETAILED};

/// Event ID of the "IVPN Client detected" notification.
pub const EVENT_ID_IVPN: &str = "interop:ivpn";
/// Event ID of the old-version compatibility warning.
pub const EVENT_ID_IVPN_OLD_VERSION: &str = "interop:ivpn-old-version";

/// Title of the "IVPN Client detected" notification.
pub const TITLE_IVPN: &str = "IVPN Client detected";
/// Message of the "IVPN Client detected" notification.
pub const MESSAGE_IVPN: &str =
    "Portmaster has detected the IVPN Client and will allow its VPN and service connections.";

/// Title of the old-version compatibility warning.
pub const TITLE_OLD_VERSION: &str = "IVPN Client Compatibility Notice";
/// Message of the old-version compatibility warning.
pub const MESSAGE_OLD_VERSION: &str = "Portmaster has detected the IVPN Client, but the installed version may not be fully compatible when running alongside Portmaster. Some features may not work as expected. Please consider updating to the latest version of the IVPN Client.";

/// ID of the acknowledgement action.
pub const ACTION_ID_ACK: &str = "ack";
/// Text of the acknowledgement action.
pub const ACTION_TEXT_ACK: &str = "OK";
/// ID of the "don't show again" action.
pub const ACTION_ID_SUPPRESS: &str = "suppress";
/// Text of the "don't show again" action.
pub const ACTION_TEXT_SUPPRESS: &str = "Don't show again";

/// How long the old-version warning stays visible: five minutes.
pub const OLD_VERSION_EXPIRY_SECS: i64 = 5 * 60;

/// Database key used to persist the user's choice to suppress the IVPN
/// detected notification. Mirrors `Notification_DB_ID_IvpnDetectSuppressed`.
pub const NOTIFICATION_DB_ID_IVPN_DETECT_SUPPRESSED: &str =
    "core:notifications/interop/ivpn/suppressed";

/// Builds the "IVPN Client detected" notification, like Go
/// `initAndShowNotification` before `Notify`.
pub fn build_ivpn_notification() -> Notification {
    Notification::info(EVENT_ID_IVPN, TITLE_IVPN, MESSAGE_IVPN)
        .with_action(Action::new(ACTION_ID_ACK, ACTION_TEXT_ACK))
        .with_action(
            Action::new(ACTION_ID_SUPPRESS, ACTION_TEXT_SUPPRESS)
                .with_visibility(ACTION_VISIBILITY_DETAILED),
        )
}

/// Builds the old-version warning, like Go `showNotificationWarnOldVersion`.
///
/// `expires` is a Unix timestamp; the Go code uses `now + 5 minutes`.
pub fn build_old_version_notification(expires: i64) -> Notification {
    Notification::warning(EVENT_ID_IVPN_OLD_VERSION, TITLE_OLD_VERSION, MESSAGE_OLD_VERSION)
        .with_action(Action::new(ACTION_ID_ACK, ACTION_TEXT_ACK))
        .with_expiry(expires)
}

/// Installs the action handler of the "IVPN Client detected" notification.
///
/// Go's handler reads `SelectedActionID`, persists the suppression decision if
/// requested, and always deletes the notification. The Rust registry already
/// fills in `selected_action_id` on the snapshot passed to the handler.
pub fn attach_suppress_action<F>(
    notifications: Arc<Notifications>,
    event_id: &str,
    on_suppress: F,
) where
    F: Fn() -> Result<(), String> + Send + Sync + 'static,
{
    let Some(handle) = notifications.get(event_id) else {
        return;
    };
    let delete_notifications = Arc::clone(&notifications);
    handle.set_action_handler(Arc::new(move |notification: &Notification| {
        if notification.selected_action_id == ACTION_ID_SUPPRESS {
            on_suppress()?;
        }
        delete_notifications.delete(&notification.event_id);
        Ok(())
    }));
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ivpn_notification_matches_go() {
        let notification = build_ivpn_notification();
        assert_eq!(notification.event_id, "interop:ivpn");
        assert_eq!(notification.type_, 0); // notifications.Info
        assert_eq!(notification.title, "IVPN Client detected");
        assert_eq!(
            notification.message,
            "Portmaster has detected the IVPN Client and will allow its VPN and service connections."
        );
        assert!(!notification.show_on_system);
        assert_eq!(notification.expires, 0);
        assert_eq!(notification.available_actions.len(), 2);
        assert_eq!(notification.available_actions[0].id, "ack");
        assert_eq!(notification.available_actions[0].text, "OK");
        assert_eq!(notification.available_actions[1].id, "suppress");
        assert_eq!(notification.available_actions[1].text, "Don't show again");
        assert_eq!(
            notification.available_actions[1].visibility,
            ACTION_VISIBILITY_DETAILED
        );
    }

    #[test]
    fn old_version_notification_matches_go() {
        let notification = build_old_version_notification(123456);
        assert_eq!(notification.event_id, "interop:ivpn-old-version");
        assert_eq!(notification.type_, 1); // notifications.Warning
        assert_eq!(notification.title, "IVPN Client Compatibility Notice");
        assert_eq!(notification.message, MESSAGE_OLD_VERSION);
        assert!(notification.show_on_system);
        assert_eq!(notification.expires, 123456);
        assert_eq!(notification.available_actions.len(), 1);
        assert_eq!(notification.available_actions[0].id, "ack");
        assert_eq!(notification.available_actions[0].text, "OK");
    }

    #[test]
    fn suppress_action_persists_and_deletes() {
        let notifications = Arc::new(Notifications::new());
        let suppressed = Arc::new(std::sync::atomic::AtomicBool::new(false));
        let flag = Arc::clone(&suppressed);
        notifications.notify(build_ivpn_notification());
        attach_suppress_action(Arc::clone(&notifications), EVENT_ID_IVPN, move || {
            flag.store(true, std::sync::atomic::Ordering::SeqCst);
            Ok(())
        });

        assert!(notifications.select_action(EVENT_ID_IVPN, ACTION_ID_SUPPRESS));
        assert!(suppressed.load(std::sync::atomic::Ordering::SeqCst));
        assert!(notifications.get(EVENT_ID_IVPN).is_none());
    }

    #[test]
    fn ack_action_deletes_without_suppressing() {
        let notifications = Arc::new(Notifications::new());
        let suppressed = Arc::new(std::sync::atomic::AtomicBool::new(false));
        let flag = Arc::clone(&suppressed);
        notifications.notify(build_ivpn_notification());
        attach_suppress_action(Arc::clone(&notifications), EVENT_ID_IVPN, move || {
            flag.store(true, std::sync::atomic::Ordering::SeqCst);
            Ok(())
        });

        assert!(notifications.select_action(EVENT_ID_IVPN, ACTION_ID_ACK));
        assert!(!suppressed.load(std::sync::atomic::Ordering::SeqCst));
        assert!(notifications.get(EVENT_ID_IVPN).is_none());
    }
}
