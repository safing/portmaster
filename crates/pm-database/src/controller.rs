//! Database controller: hooks, subscriptions and record state handling.

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, RwLock, Weak};

use pm_query::Query;
use pm_record::Record;
use tokio::sync::mpsc::{self, Receiver, Sender};

use crate::storage::Storage;
use crate::{now, DatabaseInfo, Error, Result};

/// Capacity of a subscription feed, matching Go's `make(chan record.Record, 1000)`.
const SUBSCRIPTION_BUFFER: usize = 1000;

/// A hook registered for records matching a query.
pub trait Hook: Send + Sync {
    /// Whether `pre_get` should be called.
    fn uses_pre_get(&self) -> bool {
        false
    }

    /// Called before a record is loaded from storage.
    fn pre_get(&self, _db_key: &str) -> Result<()> {
        Ok(())
    }

    /// Whether `post_get` should be called.
    fn uses_post_get(&self) -> bool {
        false
    }

    /// Called after a record was loaded from storage.
    fn post_get(&self, record: Record) -> Result<Record> {
        Ok(record)
    }

    /// Whether `pre_put` should be called.
    fn uses_pre_put(&self) -> bool {
        false
    }

    /// Called before a record is saved to storage.
    fn pre_put(&self, record: Record) -> Result<Record> {
        Ok(record)
    }
}

struct RegisteredHook {
    id: u64,
    query: Query,
    hook: Arc<dyn Hook>,
}

struct SubscriptionEntry {
    id: u64,
    query: Query,
    local: bool,
    internal: bool,
    sender: Sender<Record>,
}

/// Handle of a registered hook; dropping it keeps the hook active, use
/// [`HookHandle::cancel`] to unregister.
pub struct HookHandle {
    controller: Weak<Controller>,
    id: u64,
}

impl HookHandle {
    /// Unregisters the hook.
    pub fn cancel(&self) {
        if let Some(controller) = self.controller.upgrade() {
            controller.remove_hook(self.id);
        }
    }
}

/// Takes care of the extra database logic around a storage.
pub struct Controller {
    info: DatabaseInfo,
    storage: Box<dyn Storage>,
    shadow_delete: bool,
    hooks: RwLock<Vec<RegisteredHook>>,
    subscriptions: RwLock<Vec<SubscriptionEntry>>,
    shutting_down: Arc<AtomicBool>,
    next_id: Arc<AtomicU64>,
}

impl Controller {
    pub(crate) fn new(
        info: DatabaseInfo,
        storage: Box<dyn Storage>,
        shutting_down: Arc<AtomicBool>,
        next_id: Arc<AtomicU64>,
    ) -> Controller {
        Controller {
            shadow_delete: info.shadow_delete,
            info,
            storage,
            hooks: RwLock::new(Vec::new()),
            subscriptions: RwLock::new(Vec::new()),
            shutting_down,
            next_id,
        }
    }

    /// Information about the database this controller serves.
    pub fn info(&self) -> &DatabaseInfo {
        &self.info
    }

    pub fn read_only(&self) -> bool {
        self.storage.read_only()
    }

    pub fn injected(&self) -> bool {
        self.storage.injected()
    }

    fn check_running(&self) -> Result<()> {
        if self.shutting_down.load(Ordering::SeqCst) {
            return Err(Error::ShuttingDown);
        }
        Ok(())
    }

    /// Returns the record with the given database-local key.
    pub fn get(&self, db_key: &str) -> Result<Record> {
        self.check_running()?;
        self.run_pre_get_hooks(db_key)?;

        let record = self.storage.get(db_key).map_err(|err| match err {
            Error::StorageNotFound => Error::NotFound,
            other => other,
        })?;

        let record = self.run_post_get_hooks(record)?;
        if !record.meta.check_validity(now()) {
            return Err(Error::NotFound);
        }
        Ok(record)
    }

    /// Returns only the metadata of the record with the given key.
    pub fn get_meta(&self, db_key: &str) -> Result<pm_record::Meta> {
        self.check_running()?;
        let record = self.storage.get(db_key).map_err(|err| match err {
            Error::StorageNotFound => Error::NotFound,
            other => other,
        })?;
        if !record.meta.check_validity(now()) {
            return Err(Error::NotFound);
        }
        Ok(record.meta)
    }

    /// Returns the stored record without running pre-get/post-get hooks.
    ///
    /// Write paths use this instead of [`Controller::get`]: the record is about
    /// to be modified and written back, so running read hooks would transform
    /// the stored representation. Go avoids this by serving write paths from
    /// the interface cache (or via the metadata-only `GetMeta`).
    pub fn get_for_write(&self, db_key: &str) -> Result<Record> {
        self.check_running()?;
        let record = self.storage.get(db_key).map_err(|err| match err {
            Error::StorageNotFound => Error::NotFound,
            other => other,
        })?;
        if !record.meta.check_validity(now()) {
            return Err(Error::NotFound);
        }
        Ok(record)
    }

    /// Saves a record, running pre-put hooks and notifying subscribers.
    pub fn put(&self, mut record: Record) -> Result<()> {
        self.check_running()?;
        if self.read_only() {
            return Err(Error::ReadOnly);
        }

        record = self.run_pre_put_hooks(record)?;

        if !self.shadow_delete && record.meta.is_deleted() {
            self.storage.delete(&record.key.key)?;
        } else {
            record = self.storage.put(record)?;
        }

        self.notify_subscribers(&record);
        Ok(())
    }

    /// Pushes a record update to subscribers without touching storage.
    pub fn push_update(&self, record: &Record) {
        if self.shutting_down.load(Ordering::SeqCst) {
            return;
        }
        self.notify_subscribers(record);
    }

    /// Executes a query on the storage.
    pub fn query(&self, query: &Query, local: bool, internal: bool) -> Result<Vec<Record>> {
        self.check_running()?;
        self.storage.query(query, local, internal)
    }

    /// Registers a hook for records matching the query.
    pub fn register_hook(self: &Arc<Self>, query: Query, hook: Arc<dyn Hook>) -> HookHandle {
        let id = self.next_id.fetch_add(1, Ordering::SeqCst);
        self.hooks
            .write()
            .expect("lock poisoned")
            .push(RegisteredHook { id, query, hook });
        HookHandle {
            controller: Arc::downgrade(self),
            id,
        }
    }

    fn remove_hook(&self, id: u64) {
        self.hooks
            .write()
            .expect("lock poisoned")
            .retain(|hook| hook.id != id);
    }

    pub(crate) fn subscribe(
        &self,
        query: Query,
        local: bool,
        internal: bool,
    ) -> Result<(u64, Receiver<Record>)> {
        self.check_running()?;
        let (sender, receiver) = mpsc::channel(SUBSCRIPTION_BUFFER);
        let id = self.next_id.fetch_add(1, Ordering::SeqCst);
        self.subscriptions
            .write()
            .expect("lock poisoned")
            .push(SubscriptionEntry {
                id,
                query,
                local,
                internal,
                sender,
            });
        Ok((id, receiver))
    }

    pub(crate) fn remove_subscription(&self, id: u64) {
        self.subscriptions
            .write()
            .expect("lock poisoned")
            .retain(|subscription| subscription.id != id);
    }

    pub fn maintain(&self) -> Result<()> {
        self.check_running()?;
        self.storage.maintain()
    }

    pub fn maintain_thorough(&self) -> Result<()> {
        self.check_running()?;
        self.storage.maintain_thorough()
    }

    pub fn maintain_record_states(&self, purge_deleted_before: i64) -> Result<()> {
        self.check_running()?;
        self.storage
            .maintain_record_states(purge_deleted_before, self.shadow_delete)
    }

    pub fn shutdown(&self) -> Result<()> {
        self.storage.shutdown()
    }

    fn notify_subscribers(&self, record: &Record) {
        let subscriptions = self.subscriptions.read().expect("lock poisoned");
        for subscription in subscriptions.iter() {
            if record
                .meta
                .check_permission(subscription.local, subscription.internal)
                && subscription.query.matches(record)
            {
                // Drop the update when the feed is full or closed, like Go's
                // non-blocking send on the buffered subscription channel.
                let _ = subscription.sender.try_send(record.clone());
            }
        }
    }

    fn run_pre_get_hooks(&self, db_key: &str) -> Result<()> {
        let hooks = self.hooks.read().expect("lock poisoned");
        for registered in hooks.iter() {
            if !registered.hook.uses_pre_get() || !registered.query.matches_key(db_key) {
                continue;
            }
            registered.hook.pre_get(db_key)?;
        }
        Ok(())
    }

    fn run_post_get_hooks(&self, mut record: Record) -> Result<Record> {
        let hooks = self.hooks.read().expect("lock poisoned");
        for registered in hooks.iter() {
            if !registered.hook.uses_post_get() || !registered.query.matches(&record) {
                continue;
            }
            record = registered.hook.post_get(record)?;
        }
        Ok(record)
    }

    fn run_pre_put_hooks(&self, mut record: Record) -> Result<Record> {
        let hooks = self.hooks.read().expect("lock poisoned");
        for registered in hooks.iter() {
            if !registered.hook.uses_pre_put() || !registered.query.matches(&record) {
                continue;
            }
            record = registered.hook.pre_put(record)?;
        }
        Ok(record)
    }
}
