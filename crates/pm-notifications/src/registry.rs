//! The notification registry and lifecycle.

use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, RwLock};
use std::time::{SystemTime, UNIX_EPOCH};

use pm_database::{Controller, Key, Meta, Record};
use tokio::sync::watch;
use uuid::Uuid;

use crate::{Action, Notification, STATE_ACTIVE, STATE_EXECUTED, STATE_RESPONDED};

/// Database namespace of all notifications.
pub const NOTIFICATIONS_DB_PATH: &str = "all/";

/// Errors of the notification registry.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("invalid data, must be a notification object")]
    InvalidData,
    #[error("invalid path")]
    InvalidPath,
    #[error("notifications may not be deleted, they must be handled")]
    NoDelete,
    #[error("action already executed")]
    AlreadyExecuted,
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),
}

/// Result type of the notification registry.
pub type Result<T> = std::result::Result<T, Error>;

/// Handler that is called when the user selects an action.
pub type ActionHandler =
    Arc<dyn Fn(&Notification) -> std::result::Result<(), String> + Send + Sync>;

/// A notification with its runtime state.
pub struct NotificationHandle {
    notification: Mutex<Notification>,
    meta: Mutex<Meta>,
    response: watch::Sender<Option<String>>,
    expired: watch::Sender<bool>,
    action_handler: Mutex<Option<ActionHandler>>,
}

impl NotificationHandle {
    /// Returns a copy of the notification data.
    pub fn snapshot(&self) -> Notification {
        self.notification.lock().expect("lock poisoned").clone()
    }

    /// Returns the metadata of the notification record.
    pub fn meta(&self) -> Meta {
        *self.meta.lock().expect("lock poisoned")
    }

    /// Returns whether the notification was deleted.
    pub fn is_deleted(&self) -> bool {
        self.meta.lock().expect("lock poisoned").is_deleted()
    }

    /// Returns a receiver for the selected action ID.
    pub fn response(&self) -> watch::Receiver<Option<String>> {
        self.response.subscribe()
    }

    /// Returns a receiver that is notified when the notification expires.
    pub fn expired(&self) -> watch::Receiver<bool> {
        self.expired.subscribe()
    }

    /// Sets the handler that is called when an action is selected.
    pub fn set_action_handler(&self, handler: ActionHandler) {
        *self.action_handler.lock().expect("lock poisoned") = Some(handler);
    }

    /// Returns the database record of the notification.
    pub fn to_record(&self) -> Result<Record> {
        let notification = self.snapshot();
        let meta = self.meta();
        let data = if meta.is_deleted() || notification.message.is_empty() {
            Vec::new()
        } else {
            pm_dsd::gojson::to_vec(&notification)?
        };
        Ok(Record {
            key: Key::new(
                "notifications",
                &format!("{NOTIFICATIONS_DB_PATH}{}", notification.event_id),
            ),
            meta,
            format: pm_dsd::format::JSON,
            data,
        })
    }
}

/// The registry of all notifications.
pub struct Notifications {
    notifications: RwLock<HashMap<String, Arc<NotificationHandle>>>,
    db: RwLock<Option<Arc<Controller>>>,
    use_system_notifications: AtomicBool,
    instance_uuid: Uuid,
}

impl Default for Notifications {
    fn default() -> Self {
        Notifications::new()
    }
}

impl Notifications {
    /// Creates an empty registry.
    pub fn new() -> Notifications {
        Notifications {
            notifications: RwLock::new(HashMap::new()),
            db: RwLock::new(None),
            use_system_notifications: AtomicBool::new(true),
            instance_uuid: Uuid::new_v4(),
        }
    }

    /// Sets the database controller that receives notification updates.
    pub fn set_db_controller(&self, controller: Arc<Controller>) {
        *self.db.write().expect("lock poisoned") = Some(controller);
    }

    /// Sets whether notifications may be shown on the system.
    pub fn set_use_system_notifications(&self, enabled: bool) {
        self.use_system_notifications
            .store(enabled, Ordering::SeqCst);
    }

    pub fn use_system_notifications(&self) -> bool {
        self.use_system_notifications.load(Ordering::SeqCst)
    }

    /// Returns the notification with the given event ID.
    pub fn get(&self, event_id: &str) -> Option<Arc<NotificationHandle>> {
        self.notifications
            .read()
            .expect("lock poisoned")
            .get(event_id)
            .cloned()
    }

    /// Returns all notifications.
    pub fn all(&self) -> Vec<Arc<NotificationHandle>> {
        self.notifications
            .read()
            .expect("lock poisoned")
            .values()
            .cloned()
            .collect()
    }

    /// Sends the given notification and returns its handle.
    pub fn notify(&self, notification: Notification) -> Option<Arc<NotificationHandle>> {
        self.save(notification, true)
    }

    /// Sends an info notification, adding a default "OK" action.
    pub fn notify_info(
        &self,
        id: &str,
        title: &str,
        message: &str,
    ) -> Option<Arc<NotificationHandle>> {
        self.notify(with_default_action(Notification::info(id, title, message)))
    }

    /// Sends a warning notification.
    pub fn notify_warning(
        &self,
        id: &str,
        title: &str,
        message: &str,
    ) -> Option<Arc<NotificationHandle>> {
        self.notify(with_default_action(Notification::warning(
            id, title, message,
        )))
    }

    /// Sends an error notification.
    pub fn notify_error(
        &self,
        id: &str,
        title: &str,
        message: &str,
    ) -> Option<Arc<NotificationHandle>> {
        self.notify(with_default_action(Notification::error(id, title, message)))
    }

    /// Sends a prompt notification.
    pub fn notify_prompt(
        &self,
        id: &str,
        title: &str,
        message: &str,
    ) -> Option<Arc<NotificationHandle>> {
        self.notify(with_default_action(Notification::prompt(
            id, title, message,
        )))
    }

    /// Deletes the notification with the given event ID.
    pub fn delete(&self, event_id: &str) {
        if let Some(handle) = self.get(event_id) {
            self.delete_handle(&handle, true);
        }
    }

    /// Deletes the notification.
    pub fn delete_handle(&self, handle: &Arc<NotificationHandle>, push_update: bool) {
        {
            let mut notification = handle.notification.lock().expect("lock poisoned");
            let mut meta = handle.meta.lock().expect("lock poisoned");
            if meta.is_deleted() {
                return;
            }
            meta.delete(now());
            let _ = &mut notification;
        }

        // Notify waiters that the notification is gone. Go closes the action
        // trigger channel here, so a pending prompt must wake with "dismissed"
        // instead of waiting for the full decision timeout.
        let _ = handle.expired.send(true);
        let _ = handle.response.send(None);
        self.notifications
            .write()
            .expect("lock poisoned")
            .remove(&handle.snapshot().event_id);

        if push_update {
            self.push_update(handle);
        }
    }

    /// Updates the expiry of an active notification, like Go `Notification.Update`.
    pub fn update_expiry(&self, event_id: &str, expires: i64) {
        self.update_expiry_at(event_id, expires, now());
    }

    /// Updates the expiry of an active notification, using the given time for the
    /// rate limit of Go `Notification.Update`.
    ///
    /// Go refuses to update a notification that was modified less than ten
    /// seconds ago, even when it was just created.
    pub fn update_expiry_at(&self, event_id: &str, expires: i64, now_secs: i64) {
        let Some(handle) = self.get(event_id) else {
            return;
        };
        {
            let mut notification = handle.notification.lock().expect("lock poisoned");
            let meta = handle.meta.lock().expect("lock poisoned");
            if notification.state != STATE_ACTIVE {
                return;
            }
            // Do not update too quickly.
            if meta.modified > now_secs - 10 {
                return;
            }
            notification.expires = expires;
        }
        let mut meta = handle.meta.lock().expect("lock poisoned");
        meta.modified = now_secs;
        drop(meta);
        self.push_update(&handle);
    }

    /// Selects the action with the given ID and executes it.
    pub fn select_action(&self, event_id: &str, action_id: &str) -> bool {
        let Some(handle) = self.get(event_id) else {
            return false;
        };

        let handler = {
            let mut notification = handle.notification.lock().expect("lock poisoned");
            if notification.state != STATE_ACTIVE {
                return false;
            }
            notification.state = STATE_RESPONDED.to_string();
            notification.selected_action_id = action_id.to_string();
            handle.action_handler.lock().expect("lock poisoned").clone()
        };

        // Go dispatches the action function asynchronously and counts that as
        // executed, regardless of the eventual result of the handler.
        let mut executed = false;
        if let Some(handler) = handler {
            let snapshot = handle.snapshot();
            if let Err(error) = handler(&snapshot) {
                tracing::warn!(%error, event_id, "notifications: action failed");
            }
            executed = true;
        }

        // Wake up everyone that waits for the response. Like Go's action
        // trigger channel, a send only counts as executed if someone listens.
        if handle.response.send(Some(action_id.to_string())).is_ok() {
            executed = true;
        }

        {
            let mut notification = handle.notification.lock().expect("lock poisoned");
            if executed && notification.state == STATE_RESPONDED {
                notification.state = STATE_EXECUTED.to_string();
            }
        }
        let mut meta = handle.meta.lock().expect("lock poisoned");
        meta.modified = now();
        drop(meta);
        self.push_update(&handle);
        true
    }

    /// Applies an update from the database, like Go `applyUpdate`.
    pub fn apply_update(&self, incoming: Notification, key: &str) -> Result<Notification> {
        let Some(event_id) = key.strip_prefix(NOTIFICATIONS_DB_PATH) else {
            return Err(Error::InvalidPath);
        };

        let Some(existing) = self.get(event_id) else {
            // A completely new notification. Go saves the incoming object in
            // place, so the state and ID are filled in before it is returned.
            return Ok(self
                .save(incoming, false)
                .map(|handle| handle.snapshot())
                .unwrap_or_default());
        };
        if existing.is_deleted() {
            return Ok(self
                .save(incoming, false)
                .map(|handle| handle.snapshot())
                .unwrap_or_default());
        }

        {
            let mut notification = existing.notification.lock().expect("lock poisoned");
            if notification.state == STATE_EXECUTED {
                return Err(Error::AlreadyExecuted);
            }
            // An action that was executed externally.
            if incoming.state == STATE_EXECUTED {
                notification.state = STATE_EXECUTED.to_string();
                if !incoming.selected_action_id.is_empty() {
                    notification.selected_action_id = incoming.selected_action_id.clone();
                }
            }
        }

        // A selected action is executed when the notification is still active.
        if !incoming.selected_action_id.is_empty() && existing.snapshot().state == STATE_ACTIVE {
            self.select_action(event_id, &incoming.selected_action_id);
        }

        Ok(existing.snapshot())
    }

    /// Deletes all expired notifications.
    pub fn clean_expired(&self) {
        let now = now();
        for handle in self.all() {
            if handle.snapshot().is_expired(now) {
                self.delete_handle(&handle, true);
            }
        }
    }

    /// Saves a notification, like Go `Notification.save`.
    pub fn save(
        &self,
        mut notification: Notification,
        push_update: bool,
    ) -> Option<Arc<NotificationHandle>> {
        // Notifications without a title and message are ignored.
        if notification.title.is_empty() && notification.message.is_empty() {
            tracing::warn!("notifications: ignoring notification without Title or Message");
            return None;
        }

        // Derive an event ID from the message if none was given.
        if notification.event_id.is_empty() {
            let uuid = Uuid::new_v5(&self.instance_uuid, notification.message.as_bytes());
            notification.event_id = format!("unknown:{uuid}");
        }

        // Generate a random GUID if not set.
        if notification.guid.is_empty() {
            notification.guid = Uuid::new_v4().to_string();
        }

        // Notifications always start in the active state.
        if notification.state.is_empty() {
            notification.state = STATE_ACTIVE.to_string();
        }

        let is_new = self.get(&notification.event_id).is_none();
        if is_new {
            // Check if notifications may be shown on the system at all.
            if !self.use_system_notifications() {
                notification.show_on_system = false;
            }
        }

        let event_id = notification.event_id.clone();
        let handle = Arc::new(NotificationHandle {
            notification: Mutex::new(notification),
            meta: Mutex::new(Meta::default()),
            response: watch::channel(None).0,
            expired: watch::channel(false).0,
            action_handler: Mutex::new(None),
        });
        {
            let mut meta = handle.meta.lock().expect("lock poisoned");
            meta.modified = now();
            meta.created = meta.modified;
        }

        self.notifications
            .write()
            .expect("lock poisoned")
            .insert(event_id, Arc::clone(&handle));

        if push_update {
            self.push_update(&handle);
        }
        Some(handle)
    }

    fn push_update(&self, handle: &Arc<NotificationHandle>) {
        let controller = self.db.read().expect("lock poisoned").clone();
        let Some(controller) = controller else {
            return;
        };
        match handle.to_record() {
            Ok(record) => controller.push_update(&record),
            Err(error) => {
                tracing::error!(%error, "notifications: failed to serialize notification");
            }
        }
    }
}

/// Returns the current time in Unix seconds.
pub fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}

/// Adds the default acknowledgement action if the notification has none.
pub fn with_default_action(mut notification: Notification) -> Notification {
    if notification.available_actions.is_empty() {
        notification
            .available_actions
            .push(Action::new("ack", "OK"));
    }
    notification
}
