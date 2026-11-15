//! Compatible implementation of the Go `base/runtime` package.
//!
//! The runtime registry lets modules expose computed, read-only (or write-only)
//! values through an injected database. Providers are registered under a key or
//! a key prefix (`foo/`) and are looked up by the longest matching prefix.
//!
//! # Compatibility notes
//!
//! * The Go implementation uses a radix tree; this crate keeps providers in a
//!   `BTreeMap` and looks for the longest matching prefix by scanning
//!   backwards. The registered keys are the same, so the results are identical.
//! * Go's `ValueProvider.Set` returns `(record.Record, error)`; here it returns
//!   `Option<Record>`, where `None` means "no record returned".
//! * `Query` returns a `Vec` instead of Go's iterator, as the database layer
//!   does. Records are returned in provider order, which is lexicographic.
//! * The default registry is shared process-wide, like Go's `DefaultRegistry`.
//!   There is no `mgr.Manager`; the core wires [`Runtime::start`] itself.

mod provider;
mod registry;
mod storage;

use std::sync::OnceLock;

pub use provider::{
    provide_record, SimpleValueGetter, SimpleValueSetter, SingleRecord, TraceProvider,
    ValueProvider,
};
pub use registry::{Error, PushFunc, Registry, Result, RUNTIME_DATABASE_NAME};
pub use storage::RegistryStorage;

/// Returns the default registry of this process.
pub fn default_registry() -> &'static Registry {
    static REGISTRY: OnceLock<Registry> = OnceLock::new();
    REGISTRY.get_or_init(Registry::new)
}

/// Registers a provider at the given key or prefix on the default registry,
/// like Go `runtime.Register`.
pub fn register(
    key_or_prefix: &str,
    provider: std::sync::Arc<dyn ValueProvider>,
) -> Result<PushFunc> {
    default_registry().register(key_or_prefix, provider)
}

/// The runtime module: it injects the default registry as the `runtime`
/// database, like Go `Runtime.Start`.
#[derive(Default)]
pub struct Runtime {
    registry: Registry,
    controller: Option<std::sync::Arc<pm_database::Controller>>,
}

impl Runtime {
    /// Creates the runtime module around the default registry.
    pub fn new() -> Runtime {
        Runtime {
            registry: default_registry().clone(),
            controller: None,
        }
    }

    /// Creates the runtime module around a dedicated registry.
    pub fn with_registry(registry: Registry) -> Runtime {
        Runtime {
            registry,
            controller: None,
        }
    }

    /// Returns the registry of this module.
    pub fn registry(&self) -> &Registry {
        &self.registry
    }

    /// Returns the injected database name, if the module was started.
    pub fn database_name(&self) -> String {
        self.registry.database_name()
    }

    /// Registers and injects the `runtime` database, like Go `Runtime.Start`.
    ///
    /// The default registry can only be injected once. A second module in the
    /// same process (Go only allows one instance) gets a dedicated registry, so
    /// tests can start several cores.
    pub fn start(&mut self, databases: &pm_database::Databases) -> Result<()> {
        static START: std::sync::Mutex<()> = std::sync::Mutex::new(());
        let _guard = START
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if self.registry.is_injected() {
            self.registry = Registry::new();
        }

        databases
            .register_injected(RUNTIME_DATABASE_NAME, "Runtime database")
            .map_err(|error| Error::Database(error.to_string()))?;
        self.controller = Some(
            self.registry
                .inject_as_database(databases, RUNTIME_DATABASE_NAME)?,
        );
        Ok(())
    }

    /// Returns the controller of the injected database, if started.
    pub fn controller(&self) -> Option<std::sync::Arc<pm_database::Controller>> {
        self.controller.clone()
    }

    /// Stops the module; the injected database keeps its data.
    pub fn stop(&mut self) {
        self.controller = None;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_default_registry_is_shared() {
        let module = Runtime::new();
        assert!(module.database_name().is_empty());

        // Providers registered through the module are visible on the default
        // registry, and the other way around.
        let record = pm_record::Record {
            key: pm_record::Key::new("runtime", "default/test"),
            meta: pm_record::Meta::default(),
            format: pm_dsd::format::JSON,
            data: b"value".to_vec(),
        };
        module
            .registry()
            .register(
                "default/",
                std::sync::Arc::new(crate::SingleRecord::new(record)),
            )
            .unwrap();
        assert!(default_registry().get("default/test").is_ok());
    }
}
