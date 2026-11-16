//! Prometheus-compatible metrics registry, port of the Go `base/metrics`
//! package.
//!
//! The [`Registry`] exposes [`Counter`], [`Gauge`], [`Histogram`] and
//! [`FetchingCounter`] metrics. `WriteMetrics` reproduces the exact
//! VictoriaMetrics text format (no `# HELP`/`# TYPE` lines, because Go never
//! enables metadata): `name{labels} value\n`, sorted by labeled identifier.
//!
//! A process-wide default registry backs the free functions, mirroring the Go
//! package-level API (useful for ad-hoc metrics in tools and tests). The core
//! uses [`Metrics`], which owns a dedicated [`Registry`] instance so several
//! services can start in one process, and wires config, API, persistence and
//! shutdown. Other crates can register into that registry via
//! [`Metrics::registry`].

pub mod api;
pub mod config;
mod counter;
mod error;
mod fetching;
mod gauge;
mod histogram;
mod metric;
pub mod process;
pub mod push;
mod registry;
mod system;
mod types;

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock, RwLock};
use std::time::Duration;

use tokio::task::JoinHandle;
use tokio_util::sync::CancellationToken;

use pm_api::{Api, Permission};
use pm_config::{Config, ExpertiseLevel};
use pm_database::{Databases, Interface};
use serde_json::Value;

pub use counter::Counter;
pub use error::{Error, Result};
pub use fetching::FetchingCounter;
pub use gauge::Gauge;
pub use histogram::Histogram;
pub use metric::Metric;
pub use registry::Registry;
pub use types::{FetchFn, GaugeFn, Options};

use crate::system::HostStats;

/// Database key of the persisted metrics, like Go core.
pub const PERSISTENCE_KEY: &str = "core:metrics/storage";
/// Namespace used by the core, like the Go `portmaster-core` main.
pub const NAMESPACE: &str = "portmaster";

static DEFAULT_REGISTRY: OnceLock<Arc<Registry>> = OnceLock::new();
static HOST_STATS: OnceLock<HostStats> = OnceLock::new();

/// Returns the process-wide default registry.
pub fn default_registry() -> Arc<Registry> {
    DEFAULT_REGISTRY
        .get_or_init(|| Arc::new(Registry::new()))
        .clone()
}

/// Sets the metric namespace; must be called before the first metric is registered.
pub fn set_namespace(namespace: &str) -> Result<()> {
    default_registry().set_namespace(namespace)
}

/// Adds a global label; must be called before the first metric is registered.
pub fn add_global_label(name: &str, value: &str) -> Result<()> {
    default_registry().add_global_label(name, value)
}

/// Registers a new counter on the default registry.
pub fn new_counter(
    id: &str,
    labels: Option<BTreeMap<String, String>>,
    options: Option<Options>,
) -> Result<Arc<Counter>> {
    default_registry().new_counter(id, labels, options)
}

/// Registers a new gauge on the default registry.
pub fn new_gauge(
    id: &str,
    labels: Option<BTreeMap<String, String>>,
    function: Option<GaugeFn>,
    options: Option<Options>,
) -> Result<Arc<Gauge>> {
    default_registry().new_gauge(id, labels, function, options)
}

/// Registers a new fetching counter on the default registry.
pub fn new_fetching_counter(
    id: &str,
    labels: Option<BTreeMap<String, String>>,
    function: FetchFn,
    options: Option<Options>,
) -> Result<Arc<FetchingCounter>> {
    default_registry().new_fetching_counter(id, labels, function, options)
}

/// Registers a new histogram on the default registry.
pub fn new_histogram(
    id: &str,
    labels: Option<BTreeMap<String, String>>,
    options: Option<Options>,
) -> Result<Arc<Histogram>> {
    default_registry().new_histogram(id, labels, options)
}

/// Writes all visible metrics in Prometheus format.
pub fn write_metrics(out: &mut String, permission: Permission, expertise_level: ExpertiseLevel) {
    default_registry().write_prometheus(out, permission, expertise_level);
}

/// Exports all visible metrics with metadata and current values.
pub fn export_metrics(permission: Permission) -> Value {
    default_registry().export_metrics(permission)
}

/// Exports the values of all visible metrics.
pub fn export_values(permission: Permission, internal_only: bool) -> Value {
    default_registry().export_values(permission, internal_only)
}

/// Enables persistence on the default registry.
pub fn enable_metric_persistence(interface: Interface, key: &str) -> Result<()> {
    default_registry().enable_persistence(interface, key)
}

/// The metrics module, wired into the core service.
pub struct Metrics {
    registry: Arc<Registry>,
    values: config::ConfigValues,
    started: AtomicBool,
    scrapes: RwLock<Option<Arc<Counter>>>,
    /// Cancels the push worker, if one was started.
    push_cancel: CancellationToken,
    /// Handle of the running push worker, awaited on graceful shutdown.
    push_handle: Mutex<Option<JoinHandle<()>>>,
}

impl Metrics {
    /// Creates the module with a fresh registry, so multiple services can run
    /// in one process (as the core tests do).
    pub fn new(config: &Arc<Config>) -> Arc<Metrics> {
        Arc::new(Metrics {
            registry: Arc::new(Registry::new()),
            values: config::ConfigValues::read(config),
            started: AtomicBool::new(false),
            scrapes: RwLock::new(None),
            push_cancel: CancellationToken::new(),
            push_handle: Mutex::new(None),
        })
    }

    /// Returns the underlying registry.
    pub fn registry(&self) -> Arc<Registry> {
        Arc::clone(&self.registry)
    }

    /// Sets the metric namespace; must be called before `start`.
    pub fn set_namespace(&self, namespace: &str) -> Result<()> {
        self.registry.set_namespace(namespace)
    }

    /// Registers the API endpoints of the module.
    pub fn register_api(self: &Arc<Self>, api: &Arc<Api>) -> Result<()> {
        let scrapes = Arc::clone(self);
        api::register_api(
            api,
            Arc::clone(&self.registry),
            Arc::new(move || scrapes.note_scrape()),
        )
        .map_err(|error| Error::Api(error.to_string()))
    }

    /// Starts the module: enables persistence, registers all metrics and marks
    /// the registry ready.
    pub fn start(self: &Arc<Self>, databases: &Arc<Databases>) -> Result<()> {
        if self.started.swap(true, Ordering::SeqCst) {
            return Err(Error::AlreadyInitialized);
        }

        if !self.values.instance.is_empty() {
            self.registry
                .add_global_label("instance", &self.values.instance)?;
        }
        match self
            .registry
            .enable_persistence(databases.internal_interface(), PERSISTENCE_KEY)
        {
            Ok(()) => {}
            Err(Error::AlreadyInitialized) => {
                tracing::warn!("metrics: persistence was already enabled elsewhere");
            }
            Err(error) => return Err(error),
        }
        self.registry.mark_ready();

        let stats = HOST_STATS.get_or_init(HostStats::new);
        system::register_system_metrics(&self.registry, &self.values.comment, stats)?;
        process::register_process_metrics(&self.registry)?;

        let scrapes = self.registry.new_counter(
            "core/metrics/scrapes/total",
            None,
            Some(Options {
                name: "Metrics Scrapes".to_string(),
                permission: pm_api::PERMIT_USER,
                persist: true,
                ..Options::default()
            }),
        )?;
        *self.scrapes.write().expect("lock poisoned") = Some(scrapes);

        // Go starts a pusher when core/metrics/push is configured (`metricsWriter`).
        if !self.values.push.is_empty() {
            self.start_push_worker();
        }
        Ok(())
    }

    /// Spawns the periodic push worker when a tokio runtime is available.
    fn start_push_worker(self: &Arc<Self>) {
        let registry = Arc::clone(&self.registry);
        let render: push::RenderFn = Arc::new(move || {
            let mut body = String::new();
            registry.write_prometheus(
                &mut body,
                pm_api::PERMIT_SELF,
                pm_config::EXPERTISE_LEVEL_DEVELOPER,
            );
            body
        });
        let publisher = Arc::new(push::Publisher::new(
            self.values.push.clone(),
            render,
            Arc::new(push::HttpMetricSender::default()),
        ));
        let cancel = self.push_cancel.clone();
        let url = publisher.url().to_string();
        match tokio::runtime::Handle::try_current() {
            Ok(handle) => {
                let handle = handle.spawn(async move { publisher.run(cancel).await });
                *self.push_handle.lock().expect("lock poisoned") = Some(handle);
                tracing::info!(%url, "metrics: push worker started");
            }
            Err(_) => {
                tracing::warn!(
                    "metrics: push URL configured but no tokio runtime is available; push worker not started"
                );
            }
        }
    }

    /// Counts a `/metrics` scrape, if the counter has been registered.
    pub fn note_scrape(&self) {
        if let Some(counter) = self.scrapes.read().expect("lock poisoned").as_ref() {
            counter.inc();
        }
    }

    /// Stops the module and persists counter values, like Go `stop()`.
    ///
    /// Cancels the push worker so it stops at its next select point. Use
    /// [`Metrics::stop_and_join`] from an async context to also wait for any
    /// in-flight request to finish.
    pub fn stop(&self) {
        self.push_cancel.cancel();
        self.registry.store_persistent_metrics();
    }

    /// Cancels the push worker, waits for it to exit (bounded so a stuck request
    /// cannot hold shutdown forever) and persists counter values.
    pub async fn stop_and_join(&self) {
        self.push_cancel.cancel();
        // Take the handle out of the lock scope before awaiting, otherwise the
        // MutexGuard would be held across the await and make this future !Send.
        let handle = self.push_handle.lock().expect("lock poisoned").take();
        if let Some(handle) = handle {
            if tokio::time::timeout(Duration::from_secs(2), handle)
                .await
                .is_err()
            {
                tracing::warn!("metrics: push worker did not stop within the shutdown timeout");
            }
        }
        self.registry.store_persistent_metrics();
    }

    /// Returns whether a push worker was actually started, for diagnostics.
    pub fn push_worker_running(&self) -> bool {
        self.push_handle
            .lock()
            .expect("lock poisoned")
            .as_ref()
            .is_some_and(|handle| !handle.is_finished())
    }
}

/// Increments the number of warning log lines reported by the `logs` metric.
pub fn inc_warning_log_line() {
    system::log_counters::inc_warning();
}

/// Increments the number of error log lines reported by the `logs` metric.
pub fn inc_error_log_line() {
    system::log_counters::inc_error();
}

/// Increments the number of critical log lines reported by the `logs` metric.
pub fn inc_critical_log_line() {
    system::log_counters::inc_critical();
}
