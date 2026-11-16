//! Bridges netenv notification events to the core notification registry.

use std::sync::{Arc, Mutex};

use pm_netenv::{
    NotificationChange, NotificationEvent, NotificationSink, CAPTIVE_PORTAL_EVENT_ID,
};
use pm_notifications::{Action, Notification, Notifications};

/// Sends network environment notifications through `pm-notifications`.
pub struct NetEnvNotificationSink {
    notifications: Arc<Notifications>,
    active_status: Mutex<Option<String>>,
    portal_active: Mutex<bool>,
}

impl NetEnvNotificationSink {
    /// Creates the sink.
    pub fn new(notifications: Arc<Notifications>) -> NetEnvNotificationSink {
        NetEnvNotificationSink {
            notifications,
            active_status: Mutex::new(None),
            portal_active: Mutex::new(false),
        }
    }

    fn send(&self, event: &NotificationEvent) {
        let mut notification = Notification::new(
            event.type_,
            &event.event_id,
            &event.title,
            &event.message,
            event.show_on_system,
        )
        .with_event_data(event.event_data.clone());
        notification.available_actions = event
            .available_actions
            .iter()
            .map(|action| {
                let mut rust_action = Action::new(&action.id, &action.text);
                if !action.type_.is_empty() {
                    rust_action = rust_action.with_type(&action.type_, action.payload.clone());
                }
                rust_action
            })
            .collect();
        self.notifications.notify(notification);
    }
}

impl NotificationSink for NetEnvNotificationSink {
    fn handle(&self, change: NotificationChange) {
        if change.delete_online_status {
            if let Some(id) = self
                .active_status
                .lock()
                .unwrap_or_else(|error| error.into_inner())
                .take()
            {
                self.notifications.delete(&id);
            }
        }
        if change.delete_captive_portal
            && *self
                .portal_active
                .lock()
                .unwrap_or_else(|error| error.into_inner())
        {
            self.notifications.delete(CAPTIVE_PORTAL_EVENT_ID);
            *self
                .portal_active
                .lock()
                .unwrap_or_else(|error| error.into_inner()) = false;
        }

        for event in &change.notify {
            if event.event_id == CAPTIVE_PORTAL_EVENT_ID {
                *self
                    .portal_active
                    .lock()
                    .unwrap_or_else(|error| error.into_inner()) = true;
            } else {
                *self
                    .active_status
                    .lock()
                    .unwrap_or_else(|error| error.into_inner()) = Some(event.event_id.clone());
            }
            self.send(event);
        }
    }
}
