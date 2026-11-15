//! The runtime provider registry.

use std::collections::BTreeMap;
use std::sync::{Arc, RwLock};

use pm_database::{Controller, Databases};
use pm_query::Query;
use pm_record::Record;

use crate::provider::{TraceProvider, ValueProvider};

/// Name of the database the registry is injected as.
pub const RUNTIME_DATABASE_NAME: &str = "runtime";

/// Errors of the runtime registry.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("runtime record is read-only")]
    ReadOnly,
    #[error("runtime record is write-only")]
    WriteOnly,
    #[error("runtime key or prefix already used: found provider on {0}")]
    KeyTaken(String),
    #[error("runtime key not managed by any provider")]
    KeyUnmanaged,
    #[error("runtime key not managed by any provider: for key {0}")]
    KeyUnmanagedPrefix(String),
    #[error("registry already injected")]
    Injected,
    #[error("invalid query: {0}")]
    InvalidQuery(String),
    #[error("database: {0}")]
    Database(String),
    #[error("runtime record was not found")]
    NotFound,
}

impl Error {
    /// Returns whether the error is "not found", matching Go `database.ErrNotFound`.
    pub fn is_not_found(&self) -> bool {
        matches!(self, Error::NotFound)
    }
}

/// Result type of the runtime registry.
pub type Result<T> = std::result::Result<T, Error>;

/// Pushes updated records to the database subscribers, like Go `PushFunc`.
pub type PushFunc = Arc<dyn Fn(&[Record]) + Send + Sync>;

struct Inner {
    providers: RwLock<BTreeMap<String, Arc<dyn ValueProvider>>>,
    controller: RwLock<Option<Arc<Controller>>>,
    database_name: RwLock<String>,
}

impl Default for Inner {
    fn default() -> Self {
        Inner {
            providers: RwLock::new(BTreeMap::new()),
            controller: RwLock::new(None),
            database_name: RwLock::new(String::new()),
        }
    }
}

/// Keeps track of registered runtime value providers and exposes them through
/// an injected database.
///
/// Clones share the same registry, so a [`Registry`] can be handed to several
/// modules.
#[derive(Clone, Default)]
pub struct Registry {
    inner: Arc<Inner>,
}

impl std::fmt::Debug for Registry {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Registry")
            .field("keys", &self.registration_keys())
            .field("database", &self.database_name())
            .finish()
    }
}

/// Returns whether the key registers a prefix.
fn is_prefix_key(key: &str) -> bool {
    key.ends_with('/')
}

impl Registry {
    /// Creates a new registry.
    pub fn new() -> Registry {
        Registry::default()
    }

    /// Returns the name of the database the registry was injected as, or an
    /// empty string.
    pub fn database_name(&self) -> String {
        self.inner
            .database_name
            .read()
            .expect("lock poisoned")
            .clone()
    }

    /// Returns whether the registry was injected as a database.
    pub fn is_injected(&self) -> bool {
        self.inner
            .controller
            .read()
            .expect("lock poisoned")
            .is_some()
    }

    /// Injects the registry as the storage of the given database, like Go
    /// `Registry.InjectAsDatabase`.
    ///
    /// The database must be registered as an injected database beforehand.
    pub fn inject_as_database(&self, databases: &Databases, name: &str) -> Result<Arc<Controller>> {
        if self
            .inner
            .controller
            .read()
            .expect("lock poisoned")
            .is_some()
        {
            return Err(Error::Injected);
        }

        let controller = databases
            .inject(
                name,
                Box::new(crate::storage::RegistryStorage::new(self.clone())),
            )
            .map_err(|error| Error::Database(error.to_string()))?;

        *self.inner.database_name.write().expect("lock poisoned") = name.to_string();
        *self.inner.controller.write().expect("lock poisoned") = Some(Arc::clone(&controller));
        Ok(controller)
    }

    /// Registers a new value provider under `key_or_prefix`.
    ///
    /// `key_or_prefix` must end in `/` to be registered as a prefix. The
    /// returned [`PushFunc`] notifies database subscribers about new values.
    pub fn register(
        &self,
        key_or_prefix: &str,
        provider: Arc<dyn ValueProvider>,
    ) -> Result<PushFunc> {
        let mut providers = self.inner.providers.write().expect("lock poisoned");

        // There must not be a provider for a prefix that matches or is equal to
        // the new key.
        if let Some((key, _)) = providers
            .range(..=key_or_prefix.to_string())
            .rev()
            .find(|(key, _)| key_or_prefix.starts_with(key.as_str()))
        {
            if is_prefix_key(key) || key == key_or_prefix {
                return Err(Error::KeyTaken(key.clone()));
            }
        }

        // A new prefix must not contain already registered providers.
        if is_prefix_key(key_or_prefix) {
            if let Some((key, _)) = providers
                .range(key_or_prefix.to_string()..)
                .next()
                .filter(|(key, _)| key.starts_with(key_or_prefix))
            {
                return Err(Error::KeyTaken(key.clone()));
            }
        }

        providers.insert(
            key_or_prefix.to_string(),
            Arc::new(TraceProvider::new(provider)),
        );
        drop(providers);

        tracing::trace!(key_or_prefix, "runtime: registered new provider");

        let inner = Arc::clone(&self.inner);
        Ok(Arc::new(move |records: &[Record]| {
            let controller = inner.controller.read().expect("lock poisoned").clone();
            let Some(controller) = controller else {
                return;
            };
            for record in records {
                controller.push_update(record);
            }
        }))
    }

    /// Returns the runtime value identified by `key`.
    pub fn get(&self, key: &str) -> Result<Record> {
        let provider = self.matching_provider(key).ok_or(Error::NotFound)?;
        let records = match provider.get(key) {
            // Write-only providers hide their records from reads.
            Err(Error::WriteOnly) => return Err(Error::NotFound),
            result => result?,
        };

        // Get performs an exact match.
        records
            .into_iter()
            .find(|record| record.key.key == key)
            .ok_or(Error::NotFound)
    }

    /// Stores a record in the runtime database.
    ///
    /// Returns [`Error::KeyUnmanaged`] when no provider is responsible for the
    /// record key, and [`Error::ReadOnly`] when the provider rejects writes.
    pub fn put(&self, record: Record) -> Result<Option<Record>> {
        let provider = self
            .matching_provider(&record.key.key)
            .ok_or(Error::KeyUnmanaged)?;
        provider.set(record)
    }

    /// Returns all records of all providers that match the query.
    pub fn query(&self, query: &Query, local: bool, internal: bool) -> Result<Vec<Record>> {
        let checked = query
            .clone()
            .check()
            .map_err(|error| Error::InvalidQuery(error.to_string()))?;

        let search_prefix = checked.database_key_prefix();
        let providers = self.providers_by_prefix(&search_prefix);
        if providers.is_empty() {
            return Err(Error::KeyUnmanagedPrefix(search_prefix));
        }

        let now = pm_database::now();
        let mut records = Vec::new();
        for (provider_key, provider) in providers {
            let key = if search_prefix.len() > provider_key.len() {
                search_prefix.as_str()
            } else {
                provider_key.as_str()
            };

            let found = match provider.get(key) {
                // Write-only providers are skipped.
                Err(Error::WriteOnly) => continue,
                result => result?,
            };

            for record in found {
                let allowed = checked.matches_key(&record.key.key)
                    && record.meta.check_validity(now)
                    && record.meta.check_permission(local, internal)
                    && checked.matches_record(&record);
                if allowed {
                    records.push(record);
                }
            }
        }

        Ok(records)
    }

    /// Returns all provider registration keys or prefixes, sorted.
    pub fn registration_keys(&self) -> Vec<String> {
        self.inner
            .providers
            .read()
            .expect("lock poisoned")
            .keys()
            .cloned()
            .collect()
    }

    /// Returns the provider with the longest prefix matching `key`, together
    /// with its registration key.
    fn matching_provider_with_key(&self, key: &str) -> Option<(String, Arc<dyn ValueProvider>)> {
        self.inner
            .providers
            .read()
            .expect("lock poisoned")
            .range(..=key.to_string())
            .rev()
            .find(|(provider_key, _)| {
                provider_key.as_str() == key
                    || (is_prefix_key(provider_key) && key.starts_with(provider_key.as_str()))
            })
            .map(|(provider_key, provider)| (provider_key.clone(), Arc::clone(provider)))
    }

    /// Returns the provider with the longest prefix matching `key`.
    fn matching_provider(&self, key: &str) -> Option<Arc<dyn ValueProvider>> {
        self.matching_provider_with_key(key)
            .map(|(_, provider)| provider)
    }

    /// Returns the providers to ask for the given search prefix, like Go
    /// `collectProviderByPrefix`.
    fn providers_by_prefix(&self, prefix: &str) -> Vec<(String, Arc<dyn ValueProvider>)> {
        // A provider matching the prefix itself is the only one to ask.
        if let Some(provider) = self.matching_provider_with_key(prefix) {
            return vec![provider];
        }

        self.inner
            .providers
            .read()
            .expect("lock poisoned")
            .range(prefix.to_string()..)
            .take_while(|(provider_key, _)| provider_key.starts_with(prefix))
            .map(|(provider_key, provider)| (provider_key.clone(), Arc::clone(provider)))
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn prefix_keys_end_with_a_slash() {
        assert!(is_prefix_key("test/"));
        assert!(!is_prefix_key("test"));
        assert!(!is_prefix_key(""));
    }
}
