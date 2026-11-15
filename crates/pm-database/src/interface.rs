//! Database access interface with attached options.

use std::sync::{Arc, Weak};

use pm_query::Query;
use pm_record::{Key, Meta, Record};
use serde_json::Value as JsonValue;
use tokio::sync::mpsc::UnboundedReceiver;

use crate::{now, Controller, Databases, Error, Result};

/// Options that may be set for an [`Interface`].
#[derive(Debug, Clone, Default)]
pub struct Options {
    /// The interface is used by an actor on the local device.
    pub local: bool,
    /// The interface is used by an actor within the software.
    pub internal: bool,
    /// Marks all saved records as secret.
    pub always_make_secret: bool,
    /// Marks all saved records as crown jewels.
    pub always_make_crownjewel: bool,
    /// Sets a relative expiry (seconds from now) on all saved records.
    pub always_set_relative_expiry: i64,
    /// Sets an absolute expiry on all saved records.
    pub always_set_absolute_expiry: i64,
}

impl Options {
    /// Applies the options to the record metadata, like Go `Options.Apply`.
    pub fn apply(&self, record: &mut Record, now: i64) {
        record.meta.update(now);
        if self.always_make_secret {
            record.meta.make_secret();
        }
        if self.always_make_crownjewel {
            record.meta.make_crown_jewel();
        }
        if self.always_set_absolute_expiry > 0 {
            record
                .meta
                .set_absolute_expiry(self.always_set_absolute_expiry);
        } else if self.always_set_relative_expiry > 0 {
            record
                .meta
                .set_relative_expiry(self.always_set_relative_expiry);
        }
    }

    /// Returns whether the options grant the highest possible permissions.
    pub fn has_all_permissions(&self) -> bool {
        self.local && self.internal
    }

    /// Checks whether these options may access the given record.
    pub fn has_access_permission(&self, meta: &Meta) -> bool {
        self.has_all_permissions() || meta.check_permission(self.local, self.internal)
    }
}

/// A handle to the database with attached permissions and defaults.
pub struct Interface {
    dbs: Arc<Databases>,
    options: Options,
}

impl Interface {
    pub(crate) fn new(dbs: Arc<Databases>, options: Options) -> Interface {
        Interface { dbs, options }
    }

    pub fn options(&self) -> &Options {
        &self.options
    }

    fn controller_for_key(&self, key: &str) -> Result<(Arc<Controller>, String)> {
        let parsed = Key::parse(key);
        if parsed.db.is_empty() {
            return Err(Error::Other("unknown database".to_string()));
        }
        let controller = self.dbs.controller(&parsed.db)?;
        Ok((controller, parsed.key))
    }

    fn get_for_write(&self, key: &str) -> Result<(Arc<Controller>, Record)> {
        let (controller, db_key) = self.controller_for_key(key)?;
        if controller.read_only() {
            return Err(Error::ReadOnly);
        }
        let record = controller.get(&db_key)?;
        if !self.options.has_access_permission(&record.meta) {
            return Err(Error::PermissionDenied);
        }
        Ok((controller, record))
    }

    /// Returns whether a record with the given key exists.
    pub fn exists(&self, key: &str) -> Result<bool> {
        match self.get(key) {
            Ok(_) => Ok(true),
            Err(Error::NotFound) => Ok(false),
            Err(Error::PermissionDenied) => Ok(true),
            Err(err) => Err(err),
        }
    }

    /// Returns the record with the given `db:key`.
    pub fn get(&self, key: &str) -> Result<Record> {
        let (controller, db_key) = self.controller_for_key(key)?;
        let record = controller.get(&db_key)?;
        if !self.options.has_access_permission(&record.meta) {
            return Err(Error::PermissionDenied);
        }
        Ok(record)
    }

    /// Returns the metadata of the record with the given `db:key`.
    pub fn get_meta(&self, key: &str) -> Result<Meta> {
        let (controller, db_key) = self.controller_for_key(key)?;
        let meta = controller.get_meta(&db_key)?;
        if !self.options.has_access_permission(&meta) {
            return Err(Error::PermissionDenied);
        }
        Ok(meta)
    }

    /// Saves a record to the database.
    pub fn put(&self, mut record: Record) -> Result<()> {
        let controller = self.dbs.controller(&record.key.db)?;

        if !self.options.has_all_permissions() {
            match controller.get(&record.key.key) {
                Ok(existing) => {
                    if !self.options.has_access_permission(&existing.meta) {
                        return Err(Error::PermissionDenied);
                    }
                }
                Err(Error::NotFound) => {}
                Err(err) => return Err(err),
            }
        }
        if controller.read_only() {
            return Err(Error::ReadOnly);
        }

        self.options.apply(&mut record, now());
        controller.put(record)
    }

    /// Saves a record with reset timestamps, like a new record.
    pub fn put_new(&self, mut record: Record) -> Result<()> {
        record.meta.reset();
        self.put(record)
    }

    /// Sets a value inside a JSON record, like Go `Interface.InsertValue`.
    pub fn insert_value(&self, key: &str, attribute: &str, value: JsonValue) -> Result<()> {
        let (controller, mut record) = self.get_for_write(key)?;

        let mut accessor = pm_record::accessor::JsonAccessor::from_slice(&record.data)?;
        accessor.set(attribute, value)?;
        record.data = pm_dsd::gojson::to_vec(accessor.value())?;

        self.options.apply(&mut record, now());
        controller.put(record)
    }

    /// Sets an absolute expiry on a record.
    pub fn set_absolute_expiry(&self, key: &str, expiry: i64) -> Result<()> {
        let (controller, mut record) = self.get_for_write(key)?;
        self.options.apply(&mut record, now());
        record.meta.set_absolute_expiry(expiry);
        controller.put(record)
    }

    /// Sets a relative expiry on a record.
    pub fn set_relative_expiry(&self, key: &str, seconds: i64) -> Result<()> {
        let (controller, mut record) = self.get_for_write(key)?;
        self.options.apply(&mut record, now());
        record.meta.set_relative_expiry(seconds);
        controller.put(record)
    }

    /// Marks a record as secret: only internal interfaces may read it.
    pub fn make_secret(&self, key: &str) -> Result<()> {
        let (controller, mut record) = self.get_for_write(key)?;
        self.options.apply(&mut record, now());
        record.meta.make_secret();
        controller.put(record)
    }

    /// Marks a record as a crown jewel: only local interfaces may read it.
    pub fn make_crown_jewel(&self, key: &str) -> Result<()> {
        let (controller, mut record) = self.get_for_write(key)?;
        self.options.apply(&mut record, now());
        record.meta.make_crown_jewel();
        controller.put(record)
    }

    /// Deletes a record from the database.
    pub fn delete(&self, key: &str) -> Result<()> {
        let (controller, mut record) = self.get_for_write(key)?;
        self.options.apply(&mut record, now());
        record.meta.delete(now());
        controller.put(record)
    }

    /// Executes a query on the database.
    pub fn query(&self, query: &Query) -> Result<Vec<Record>> {
        let controller = self.dbs.controller(&query.database_name())?;
        controller.query(query, self.options.local, self.options.internal)
    }

    /// Subscribes to record updates matching the query.
    pub fn subscribe(&self, query: Query) -> Result<Subscription> {
        let controller = self.dbs.controller(&query.database_name())?;
        let (id, rx) =
            controller.subscribe(query.clone(), self.options.local, self.options.internal)?;
        Ok(Subscription {
            id,
            query,
            rx,
            controller: Arc::downgrade(&controller),
        })
    }
}

/// A database subscription for updates.
///
/// Dropping the subscription stops delivery; [`Subscription::cancel`]
/// additionally unregisters it from the controller.
pub struct Subscription {
    id: u64,
    query: Query,
    rx: UnboundedReceiver<Record>,
    controller: Weak<Controller>,
}

impl Subscription {
    pub fn query(&self) -> &Query {
        &self.query
    }

    /// Waits for the next update.
    pub async fn recv(&mut self) -> Option<Record> {
        self.rx.recv().await
    }

    /// Returns the next update if one is already queued.
    pub fn try_recv(&mut self) -> Option<Record> {
        self.rx.try_recv().ok()
    }

    /// Unregisters the subscription from the database.
    pub fn cancel(&mut self) {
        if let Some(controller) = self.controller.upgrade() {
            controller.remove_subscription(self.id);
        }
    }
}
