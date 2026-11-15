//! The injected storage wrapper of the runtime registry.

use pm_database::{Error as DbError, Result as DbResult, Storage};
use pm_query::Query;
use pm_record::Record;

use crate::registry::{Error, Registry};

/// Storage adapter that exposes a [`Registry`] as an injected database.
pub struct RegistryStorage {
    registry: Registry,
}

impl RegistryStorage {
    /// Creates a storage adapter for the given registry.
    pub fn new(registry: Registry) -> RegistryStorage {
        RegistryStorage { registry }
    }

    /// Returns the wrapped registry.
    pub fn registry(&self) -> &Registry {
        &self.registry
    }
}

fn to_database_error(error: Error) -> DbError {
    match error {
        Error::ReadOnly => DbError::ReadOnly,
        Error::NotFound => DbError::NotFound,
        other => DbError::Other(other.to_string()),
    }
}

impl Storage for RegistryStorage {
    fn get(&self, key: &str) -> DbResult<Record> {
        tracing::trace!(key, "runtime: storage get");
        self.registry.get(key).map_err(to_database_error)
    }

    fn put(&self, record: Record) -> DbResult<Record> {
        match self.registry.put(record.clone()) {
            Ok(Some(result)) => Ok(result),
            // Go returns a nil record here; a missing record is an error for
            // the storage interface.
            Ok(None) => Err(DbError::Other(
                "runtime provider did not return a record".to_string(),
            )),
            Err(error) => Err(to_database_error(error)),
        }
    }

    fn delete(&self, _key: &str) -> DbResult<()> {
        // Runtime values are computed; they cannot be deleted.
        Err(DbError::NotImplemented)
    }

    fn query(&self, query: &Query, local: bool, internal: bool) -> DbResult<Vec<Record>> {
        self.registry
            .query(query, local, internal)
            .map_err(to_database_error)
    }

    fn injected(&self) -> bool {
        true
    }
}
