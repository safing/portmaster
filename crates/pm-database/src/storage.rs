//! Storage interface, the injected base implementation and an in-memory store.
//!
//! Go's storage interface returns an `*iterator.Iterator`; this port returns a
//! `Vec<Record>` because M0 queries are small and the callers consume all
//! results anyway.

use std::collections::BTreeMap;
use std::sync::RwLock;

use pm_query::Query;
use pm_record::Record;

use crate::{now, Error, Result};

/// Defines the storage API of a database backend.
///
/// Keys are database-local, without the `db:` prefix.
pub trait Storage: Send + Sync {
    fn get(&self, key: &str) -> Result<Record>;
    fn put(&self, record: Record) -> Result<Record>;
    fn delete(&self, key: &str) -> Result<()>;

    /// Returns all records matching the query that the caller may access.
    fn query(&self, query: &Query, local: bool, internal: bool) -> Result<Vec<Record>>;

    fn read_only(&self) -> bool {
        false
    }

    fn injected(&self) -> bool {
        false
    }

    /// Shuts the storage down; called once when the database system stops.
    fn shutdown(&self) -> Result<()> {
        Ok(())
    }

    /// Light maintenance, run regularly.
    fn maintain(&self) -> Result<()> {
        Ok(())
    }

    /// Thorough maintenance, run rarely.
    fn maintain_thorough(&self) -> Result<()> {
        Ok(())
    }

    /// Purges expired and deleted records, or shadow-deletes the expired ones.
    fn maintain_record_states(
        &self,
        _purge_deleted_before: i64,
        _shadow_delete: bool,
    ) -> Result<()> {
        Ok(())
    }
}

/// Shared record filter of the storage implementations.
pub(crate) fn record_matches(
    record: &Record,
    query: &Query,
    local: bool,
    internal: bool,
    now: i64,
) -> bool {
    query.matches_key(&record.key.key)
        && record.meta.check_validity(now)
        && record.meta.check_permission(local, internal)
        && (!query.has_where_condition() || query.matches_record(record))
}

/// Dummy base for injected storages: every operation is not implemented.
#[derive(Debug, Default)]
pub struct InjectBase;

impl Storage for InjectBase {
    fn get(&self, _key: &str) -> Result<Record> {
        Err(Error::NotImplemented)
    }

    fn put(&self, _record: Record) -> Result<Record> {
        Err(Error::NotImplemented)
    }

    fn delete(&self, _key: &str) -> Result<()> {
        Err(Error::NotImplemented)
    }

    fn query(&self, _query: &Query, _local: bool, _internal: bool) -> Result<Vec<Record>> {
        Err(Error::NotImplemented)
    }

    fn read_only(&self) -> bool {
        true
    }

    fn injected(&self) -> bool {
        true
    }
}

/// A simple in-memory storage, mainly useful for injected databases and tests.
#[derive(Debug, Default)]
pub struct MemoryStorage {
    records: RwLock<BTreeMap<String, Record>>,
}

impl MemoryStorage {
    pub fn new() -> Self {
        Self::default()
    }

    /// Returns the number of stored records.
    pub fn len(&self) -> usize {
        self.records.read().expect("lock poisoned").len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

impl Storage for MemoryStorage {
    fn get(&self, key: &str) -> Result<Record> {
        self.records
            .read()
            .expect("lock poisoned")
            .get(key)
            .cloned()
            .ok_or(Error::StorageNotFound)
    }

    fn put(&self, record: Record) -> Result<Record> {
        self.records
            .write()
            .expect("lock poisoned")
            .insert(record.key.key.clone(), record.clone());
        Ok(record)
    }

    fn delete(&self, key: &str) -> Result<()> {
        self.records.write().expect("lock poisoned").remove(key);
        Ok(())
    }

    fn query(&self, query: &Query, local: bool, internal: bool) -> Result<Vec<Record>> {
        let now = now();
        let records = self.records.read().expect("lock poisoned");
        Ok(records
            .values()
            .filter(|record| record_matches(record, query, local, internal, now))
            .cloned()
            .collect())
    }

    fn injected(&self) -> bool {
        true
    }
}
