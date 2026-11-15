//! Compatible implementation of the Go `base/database` package.
//!
//! The crate provides a database system (`Databases`) with a registry of
//! databases, a [`Controller`] per database that runs hooks and subscriptions,
//! and an [`Interface`] that carries permissions and defaults.
//!
//! # Compatibility notes
//!
//! * The SQLite layout is shared with Go: same schema, same record encoding,
//!   same `gorp_migrations` bookkeeping. Files can be opened by either side.
//! * `Query::limit`, `Query::offset` and `Query::orderby` are parsed and
//!   printed, but — exactly like the Go SQLite storage — they are not applied
//!   to query results yet.
//! * The Go interface cache (including delayed cached writes) is not
//!   implemented; reads always go to the storage.
//! * Batch writes (`PutMany`) and purge helpers are not implemented.
//! * Queries return a `Vec` instead of Go's channel iterator.

pub mod controller;
pub mod error;
pub mod interface;
pub mod sqlite;
pub mod storage;
mod time;

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, RwLock};

pub use controller::{Controller, Hook, HookHandle};
pub use error::{Error, Result};
pub use interface::{Interface, Options, Subscription};
pub use sqlite::SqliteStorage;
pub use storage::{InjectBase, MemoryStorage, Storage};
pub use time::now;

pub use pm_query::Query;
pub use pm_record::{Key, Meta, Record};

/// Storage type of injected databases.
pub const STORAGE_TYPE_INJECTED: &str = "injected";

/// Information about a registered database.
#[derive(Debug, Clone)]
pub struct DatabaseInfo {
    pub name: String,
    pub description: String,
    pub storage_type: String,
    /// Whether deleted records are kept until purged.
    pub shadow_delete: bool,
}

fn valid_name(name: &str) -> bool {
    name.len() >= 3
        && name
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'-')
}

/// The database system: registry, open controllers and shutdown state.
pub struct Databases {
    root: Option<PathBuf>,
    registry: RwLock<HashMap<String, DatabaseInfo>>,
    controllers: RwLock<HashMap<String, Arc<Controller>>>,
    shutting_down: Arc<AtomicBool>,
    next_id: Arc<AtomicU64>,
}

impl Databases {
    /// Creates a database system that stores databases under `root`.
    pub fn new(root: impl AsRef<Path>) -> Result<Self> {
        let root = root.as_ref();
        std::fs::create_dir_all(root)?;
        Ok(Databases {
            root: Some(root.to_path_buf()),
            ..Databases::empty()
        })
    }

    /// Creates a database system without a storage location; only injected
    /// databases can be used.
    pub fn without_disk() -> Self {
        Databases::empty()
    }

    fn empty() -> Self {
        Databases {
            root: None,
            registry: RwLock::new(HashMap::new()),
            controllers: RwLock::new(HashMap::new()),
            shutting_down: Arc::new(AtomicBool::new(false)),
            next_id: Arc::new(AtomicU64::new(1)),
        }
    }

    /// Registers a database; existing registrations only update the
    /// description and the shadow-delete flag, like in Go.
    pub fn register(&self, info: DatabaseInfo) -> Result<()> {
        if !valid_name(&info.name) {
            return Err(Error::InvalidName(info.name));
        }

        let mut registry = self.registry.write().expect("lock poisoned");
        match registry.get_mut(&info.name) {
            Some(existing) => {
                existing.description = info.description;
                existing.shadow_delete = info.shadow_delete;
            }
            None => {
                registry.insert(info.name.clone(), info);
            }
        }
        Ok(())
    }

    /// Registers a database of the `injected` storage type.
    pub fn register_injected(&self, name: &str, description: &str) -> Result<()> {
        self.register(DatabaseInfo {
            name: name.to_string(),
            description: description.to_string(),
            storage_type: STORAGE_TYPE_INJECTED.to_string(),
            shadow_delete: false,
        })
    }

    /// Returns information about all registered databases, sorted by name.
    pub fn databases(&self) -> Vec<DatabaseInfo> {
        let registry = self.registry.read().expect("lock poisoned");
        let mut all: Vec<DatabaseInfo> = registry.values().cloned().collect();
        all.sort_by(|a, b| a.name.cmp(&b.name));
        all
    }

    pub fn is_registered(&self, name: &str) -> bool {
        self.registry
            .read()
            .expect("lock poisoned")
            .contains_key(name)
    }

    /// Returns the controller of a database, opening the storage on first use.
    pub fn controller(&self, name: &str) -> Result<Arc<Controller>> {
        if let Some(controller) = self.controllers.read().expect("lock poisoned").get(name) {
            return Ok(Arc::clone(controller));
        }

        let mut controllers = self.controllers.write().expect("lock poisoned");
        if let Some(controller) = controllers.get(name) {
            return Ok(Arc::clone(controller));
        }
        if self.shutting_down.load(Ordering::SeqCst) {
            return Err(Error::ShuttingDown);
        }

        let info = self
            .registry
            .read()
            .expect("lock poisoned")
            .get(name)
            .cloned()
            .ok_or_else(|| Error::NotRegistered(name.to_string()))?;

        if info.storage_type == STORAGE_TYPE_INJECTED {
            return Err(Error::NotInjected);
        }

        let location = self.location(&info.name, &info.storage_type)?;
        let storage: Box<dyn Storage> = match info.storage_type.as_str() {
            "sqlite" => Box::new(SqliteStorage::open(&info.name, location)?),
            other => {
                return Err(Error::Other(format!("storage type {other} not registered")));
            }
        };

        let controller = Arc::new(Controller::new(
            info,
            storage,
            Arc::clone(&self.shutting_down),
            Arc::clone(&self.next_id),
        ));
        controllers.insert(name.to_string(), Arc::clone(&controller));
        Ok(controller)
    }

    /// Injects an already running storage as a database.
    pub fn inject(&self, name: &str, storage: Box<dyn Storage>) -> Result<Arc<Controller>> {
        let mut controllers = self.controllers.write().expect("lock poisoned");
        if self.shutting_down.load(Ordering::SeqCst) {
            return Err(Error::ShuttingDown);
        }
        if controllers.contains_key(name) {
            return Err(Error::AlreadyLoaded(name.to_string()));
        }

        let mut info = self
            .registry
            .read()
            .expect("lock poisoned")
            .get(name)
            .cloned()
            .ok_or_else(|| Error::NotRegistered(name.to_string()))?;
        if info.storage_type != STORAGE_TYPE_INJECTED {
            return Err(Error::NotInjectedType);
        }
        info.shadow_delete = false;

        let controller = Arc::new(Controller::new(
            info,
            storage,
            Arc::clone(&self.shutting_down),
            Arc::clone(&self.next_id),
        ));
        controllers.insert(name.to_string(), Arc::clone(&controller));
        Ok(controller)
    }

    /// Withdraws an injected database, keeping it registered.
    pub fn withdraw(&self, name: &str) {
        let injected = self
            .controllers
            .read()
            .expect("lock poisoned")
            .get(name)
            .map(|controller| controller.injected())
            .unwrap_or(false);
        if injected {
            self.controllers
                .write()
                .expect("lock poisoned")
                .remove(name);
        }
    }

    /// Returns an interface to the database system with the given options.
    pub fn interface(self: &Arc<Self>, options: Options) -> Interface {
        Interface::new(Arc::clone(self), options)
    }

    /// Returns an interface with all permissions, as used by internal modules.
    pub fn internal_interface(self: &Arc<Self>) -> Interface {
        self.interface(Options {
            local: true,
            internal: true,
            ..Options::default()
        })
    }

    /// Runs light maintenance on all open databases.
    pub fn maintain(&self) -> Result<()> {
        for controller in self.snapshot() {
            controller.maintain()?;
        }
        Ok(())
    }

    /// Runs thorough maintenance on all open databases.
    pub fn maintain_thorough(&self) -> Result<()> {
        for controller in self.snapshot() {
            controller.maintain_thorough()?;
        }
        Ok(())
    }

    /// Maintains record states on all open databases.
    pub fn maintain_record_states(&self) -> Result<()> {
        let purge_deleted_before = now();
        for controller in self.snapshot() {
            controller.maintain_record_states(purge_deleted_before)?;
        }
        Ok(())
    }

    /// Shuts down all open databases.
    pub fn shutdown(&self) -> Result<()> {
        if self.shutting_down.swap(true, Ordering::SeqCst) {
            return Ok(());
        }
        for controller in self.snapshot() {
            controller.shutdown()?;
        }
        Ok(())
    }

    fn snapshot(&self) -> Vec<Arc<Controller>> {
        self.controllers
            .read()
            .expect("lock poisoned")
            .values()
            .cloned()
            .collect()
    }

    fn location(&self, name: &str, storage_type: &str) -> Result<PathBuf> {
        let root = self
            .root
            .as_ref()
            .ok_or_else(|| Error::Other("database has no storage location".to_string()))?;
        let location = root.join(name).join(storage_type);
        std::fs::create_dir_all(&location)?;
        Ok(location)
    }
}
