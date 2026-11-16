//! The metrics registry: registration, export and persistence.

use std::collections::{BTreeMap, BTreeSet};
use std::sync::{Arc, RwLock};

use pm_api::Permission;
use pm_config::ExpertiseLevel;
use pm_database::Interface;
use pm_record::{Key, Meta, Record};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::counter::Counter;
use crate::fetching::FetchingCounter;
use crate::gauge::Gauge;
use crate::histogram::Histogram;
use crate::metric::{valid_prometheus_name, Metric, MetricBase};
use crate::types::{FetchFn, GaugeFn, MetricValue, Options};
use crate::{Error, Result};

/// The persisted form of all counters that opted into persistence.
#[derive(Debug, Default, Serialize, Deserialize)]
struct MetricsStorage {
    #[serde(rename = "Start")]
    start: i64,
    #[serde(rename = "Counters")]
    counters: BTreeMap<String, u64>,
}

/// State of an enabled persistence layer.
struct Storage {
    interface: Arc<Interface>,
    key: String,
    start: i64,
    counters: BTreeMap<String, u64>,
    loaded: bool,
}

#[derive(Default)]
struct RegistryInner {
    /// Keyed by labeled ID, which keeps metrics in Go's sorted registry order.
    metrics: BTreeMap<String, Arc<dyn Metric>>,
    internal_ids: BTreeSet<String>,
    namespace: String,
    global_labels: BTreeMap<String, String>,
    first_metric_registered: bool,
    ready_to_register: bool,
    storage: Option<Storage>,
}

/// A collection of registered metrics with namespace, global labels and
/// optional persistence.
pub struct Registry {
    inner: RwLock<RegistryInner>,
}

impl Default for Registry {
    fn default() -> Self {
        Registry::new()
    }
}

impl Registry {
    pub fn new() -> Self {
        Registry {
            inner: RwLock::new(RegistryInner::default()),
        }
    }

    /// Builds the Prometheus-compatible labeled ID and freezes the namespace
    /// and global label configuration, like Go `buildLabeledID`.
    pub(crate) fn build_labeled_id(
        &self,
        prometheus_id: &str,
        labels: &mut BTreeMap<String, String>,
    ) -> String {
        let mut inner = self.inner.write().expect("lock poisoned");
        inner.first_metric_registered = true;

        let mut metric_id = prometheus_id.trim().to_string();
        if !inner.namespace.is_empty() {
            metric_id = format!("{}_{}", inner.namespace, metric_id);
        }
        if inner.global_labels.is_empty() && labels.is_empty() {
            return metric_id;
        }
        for (name, value) in &inner.global_labels {
            labels.entry(name.clone()).or_insert_with(|| value.clone());
        }
        if labels.is_empty() {
            return metric_id;
        }
        let rendered: Vec<String> = labels
            .iter()
            .map(|(name, value)| format!("{name}={}", crate::types::go_quote(value)))
            .collect();
        format!("{}{{{}}}", metric_id, rendered.join(","))
    }

    /// Marks the registry as ready to accept metrics, like Go `start()`.
    pub fn mark_ready(&self) {
        self.inner.write().expect("lock poisoned").ready_to_register = true;
    }

    /// Sets the namespace prefix of all later metrics.
    pub fn set_namespace(&self, namespace: &str) -> Result<()> {
        let mut inner = self.inner.write().expect("lock poisoned");
        if inner.first_metric_registered {
            return Err(Error::AlreadyStarted);
        }
        if !inner.namespace.is_empty() {
            return Err(Error::AlreadySet);
        }
        inner.namespace = namespace.to_string();
        Ok(())
    }

    /// Adds a global label to all later metrics.
    pub fn add_global_label(&self, name: &str, value: &str) -> Result<()> {
        let mut inner = self.inner.write().expect("lock poisoned");
        if inner.first_metric_registered {
            return Err(Error::AlreadyStarted);
        }
        if !valid_prometheus_name(name) {
            return Err(Error::InvalidOptions(format!(
                "metric label name {name:?} must match ^[a-zA-Z_][a-zA-Z0-9_]*$"
            )));
        }
        inner.global_labels.insert(name.to_string(), value.to_string());
        Ok(())
    }

    fn register(&self, metric: Arc<dyn Metric>) -> Result<()> {
        let mut inner = self.inner.write().expect("lock poisoned");
        for registered in inner.metrics.values() {
            if registered.labeled_id() == metric.labeled_id() {
                return Err(Error::AlreadyRegistered);
            }
            if !metric.options().internal_id.is_empty()
                && metric.options().internal_id == registered.options().internal_id
            {
                return Err(Error::AlreadyRegisteredInternalId);
            }
        }
        inner
            .internal_ids
            .insert(metric.options().internal_id.clone());
        inner
            .metrics
            .insert(metric.labeled_id().to_string(), Arc::clone(&metric));
        if !inner.ready_to_register {
            return Err(Error::TooEarly(metric.id().to_string()));
        }
        inner.first_metric_registered = true;
        Ok(())
    }

    fn register_base(
        &self,
        id: &str,
        labels: Option<BTreeMap<String, String>>,
        options: Option<Options>,
    ) -> Result<MetricBase> {
        MetricBase::new(id, labels, options.unwrap_or_default(), self)
    }

    /// Registers a new counter and loads its persisted state, if any.
    pub fn new_counter(
        &self,
        id: &str,
        labels: Option<BTreeMap<String, String>>,
        options: Option<Options>,
    ) -> Result<Arc<Counter>> {
        let base = self.register_base(id, labels, options)?;
        let counter = Arc::new(Counter::new(base));
        self.register(Arc::clone(&counter) as Arc<dyn Metric>)?;
        self.apply_loaded_state(&counter);
        Ok(counter)
    }

    /// Registers a new gauge.
    pub fn new_gauge(
        &self,
        id: &str,
        labels: Option<BTreeMap<String, String>>,
        function: Option<GaugeFn>,
        options: Option<Options>,
    ) -> Result<Arc<Gauge>> {
        let base = self.register_base(id, labels, options)?;
        let gauge = Arc::new(Gauge::new(base, function));
        self.register(Arc::clone(&gauge) as Arc<dyn Metric>)?;
        Ok(gauge)
    }

    /// Registers a new fetching counter.
    pub fn new_fetching_counter(
        &self,
        id: &str,
        labels: Option<BTreeMap<String, String>>,
        function: FetchFn,
        options: Option<Options>,
    ) -> Result<Arc<FetchingCounter>> {
        let base = self.register_base(id, labels, options)?;
        let counter = Arc::new(FetchingCounter::new(base, function));
        self.register(Arc::clone(&counter) as Arc<dyn Metric>)?;
        Ok(counter)
    }

    /// Registers a new histogram.
    pub fn new_histogram(
        &self,
        id: &str,
        labels: Option<BTreeMap<String, String>>,
        options: Option<Options>,
    ) -> Result<Arc<Histogram>> {
        let base = self.register_base(id, labels, options)?;
        let histogram = Arc::new(Histogram::new(base));
        self.register(Arc::clone(&histogram) as Arc<dyn Metric>)?;
        Ok(histogram)
    }

    fn apply_loaded_state(&self, counter: &Counter) {
        let inner = self.inner.read().expect("lock poisoned");
        let Some(storage) = &inner.storage else {
            return;
        };
        if storage.loaded && counter.base().options().persist {
            counter.set(storage.counters.get(counter.base().labeled_id()).copied().unwrap_or(0));
        }
    }

    /// Writes all metrics visible to the given permission and expertise level.
    pub fn write_prometheus(
        &self,
        out: &mut String,
        permission: Permission,
        expertise_level: ExpertiseLevel,
    ) {
        let inner = self.inner.read().expect("lock poisoned");
        for metric in inner.metrics.values() {
            if permission >= metric.options().permission
                && expertise_level >= metric.options().expertise_level
            {
                metric.write_prometheus(out);
            }
        }
    }

    /// Exports all metrics visible to the given permission with their values.
    pub fn export_metrics(&self, permission: Permission) -> Value {
        let inner = self.inner.read().expect("lock poisoned");
        let metrics: Vec<Arc<dyn Metric>> = inner
            .metrics
            .values()
            .filter(|metric| permission >= metric.options().permission)
            .cloned()
            .collect();
        let mut export = Vec::with_capacity(metrics.len());
        for metric in metrics {
            export.push(json!({
                "Metric": {
                    "Identifier": metric.id(),
                    "Labels": metric.labels(),
                    "LabeledIdentifier": metric.labeled_id(),
                    "Options": metric.options(),
                },
                "CurrentValue": metric.current_value().map(MetricValue::to_json),
            }));
        }
        Value::Array(export)
    }

    /// Exports the values of all supported, visible metrics by ID.
    pub fn export_values(&self, permission: Permission, internal_only: bool) -> Value {
        let inner = self.inner.read().expect("lock poisoned");
        let metrics: Vec<Arc<dyn Metric>> = inner
            .metrics
            .values()
            .filter(|metric| permission >= metric.options().permission)
            .cloned()
            .collect();
        let mut export = serde_json::Map::new();
        for metric in metrics {
            let Some(value) = metric.current_value() else {
                continue;
            };
            let id = if !metric.options().internal_id.is_empty() {
                metric.options().internal_id.clone()
            } else if internal_only {
                continue;
            } else {
                metric.labeled_id().to_string()
            };
            export.insert(id, value.to_json());
        }
        Value::Object(export)
    }

    /// Enables persistence and loads previously stored counter values.
    pub fn enable_persistence(&self, interface: Interface, key: &str) -> Result<()> {
        {
            let inner = self.inner.read().expect("lock poisoned");
            if inner.storage.is_some() {
                return Err(Error::AlreadyInitialized);
            }
        }

        let mut loaded = MetricsStorage::default();
        let mut found = false;
        match interface.get(key) {
            Ok(record) => {
                loaded = record.decode::<MetricsStorage>().unwrap_or_default();
                found = true;
            }
            Err(pm_database::Error::NotFound) => {}
            Err(error) => {
                tracing::warn!(%error, "metrics: failed to load persisted metrics");
            }
        }

        let counters_to_restore: Vec<(String, u64)> = if found {
            loaded.counters.iter().map(|(k, v)| (k.clone(), *v)).collect()
        } else {
            Vec::new()
        };

        let mut inner = self.inner.write().expect("lock poisoned");
        if inner.storage.is_some() {
            return Err(Error::AlreadyInitialized);
        }
        inner.storage = Some(Storage {
            interface: Arc::new(interface),
            key: key.to_string(),
            start: loaded.start,
            counters: loaded.counters,
            loaded: found,
        });
        drop(inner);

        if found {
            let restore: BTreeMap<String, u64> = counters_to_restore.into_iter().collect();
            let inner = self.inner.read().expect("lock poisoned");
            for metric in inner.metrics.values() {
                if let Some(counter) = metric.as_any().downcast_ref::<Counter>() {
                    if metric.options().persist {
                        counter.set(restore.get(metric.labeled_id()).copied().unwrap_or(0));
                    }
                }
            }
        }
        Ok(())
    }

    /// Saves all persistent counters to the database.
    pub fn store_persistent_metrics(&self) {
        let (interface, key, start, counters) = {
            let inner = self.inner.read().expect("lock poisoned");
            let Some(storage) = &inner.storage else {
                return;
            };
            if storage.key.is_empty() {
                return;
            }
            let mut new_storage = MetricsStorage {
                start: pm_database::now(),
                counters: BTreeMap::new(),
            };
            if storage.loaded {
                new_storage.start = storage.start;
            }
            for metric in inner.metrics.values() {
                if metric.options().persist {
                    if let Some(counter) = metric.as_any().downcast_ref::<Counter>() {
                        new_storage
                            .counters
                            .insert(metric.labeled_id().to_string(), counter.get());
                    }
                }
            }
            (
                storage.interface.clone(),
                storage.key.clone(),
                new_storage.start,
                new_storage.counters,
            )
        };
        let storage = MetricsStorage { start, counters };
        let record = match Record::from_typed(Key::parse(&key), Meta::default(), &storage) {
            Ok(record) => record,
            Err(error) => {
                tracing::warn!(%error, "metrics: failed to encode metrics storage");
                return;
            }
        };
        if let Err(error) = interface.put(record) {
            tracing::warn!(%error, "metrics: failed to save metrics storage to db");
        }
    }

    /// Returns the number of registered metrics, for diagnostics and tests.
    pub fn len(&self) -> usize {
        self.inner.read().expect("lock poisoned").metrics.len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Returns whether persistence has been enabled.
    pub fn has_persistence(&self) -> bool {
        self.inner
            .read()
            .expect("lock poisoned")
            .storage
            .is_some()
    }
}

