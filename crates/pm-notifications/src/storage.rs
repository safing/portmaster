//! The injected `notifications` database.

use std::sync::Arc;

use pm_database::{Error as DbError, Query, Record, Result as DbResult, Storage};

use crate::registry::{Error, Notifications, NOTIFICATIONS_DB_PATH};
use crate::Notification;

/// Injected storage for the `notifications` database.
pub struct NotificationsStorage {
    notifications: Arc<Notifications>,
}

impl NotificationsStorage {
    pub fn new(notifications: Arc<Notifications>) -> NotificationsStorage {
        NotificationsStorage { notifications }
    }

    fn event_id(key: &str) -> DbResult<&str> {
        key.strip_prefix(NOTIFICATIONS_DB_PATH)
            .ok_or(DbError::StorageNotFound)
    }
}

fn map_error(error: Error) -> DbError {
    match error {
        Error::InvalidPath => DbError::StorageNotFound,
        other => DbError::Other(other.to_string()),
    }
}

impl Storage for NotificationsStorage {
    fn get(&self, key: &str) -> DbResult<Record> {
        let event_id = Self::event_id(key)?;
        let handle = self
            .notifications
            .get(event_id)
            .ok_or(DbError::StorageNotFound)?;
        handle.to_record().map_err(map_error)
    }

    fn put(&self, record: Record) -> DbResult<Record> {
        let notification: Notification = serde_json::from_slice(&record.data)
            .map_err(|error| DbError::Other(error.to_string()))?;
        let updated = self
            .notifications
            .apply_update(notification, &record.key.key)
            .map_err(map_error)?;
        let handle = self
            .notifications
            .get(&updated.event_id)
            .ok_or(DbError::StorageNotFound)?;
        handle.to_record().map_err(map_error)
    }

    fn delete(&self, key: &str) -> DbResult<()> {
        let event_id = Self::event_id(key)?;
        if self.notifications.get(event_id).is_none() {
            return Err(DbError::StorageNotFound);
        }
        self.notifications.delete(event_id);
        Ok(())
    }

    fn query(&self, query: &Query, _local: bool, _internal: bool) -> DbResult<Vec<Record>> {
        let mut records = Vec::new();
        for handle in self.notifications.all() {
            let record = handle.to_record().map_err(map_error)?;
            if record.meta.is_deleted() || !query.matches(&record) {
                continue;
            }
            records.push(record);
        }
        records.sort_by(|a, b| a.key.key.cmp(&b.key.key));
        Ok(records)
    }

    fn read_only(&self) -> bool {
        false
    }

    fn injected(&self) -> bool {
        true
    }
}
