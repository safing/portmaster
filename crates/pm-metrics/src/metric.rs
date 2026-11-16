//! The common metric interface and base implementation.

use std::any::Any;
use std::collections::BTreeMap;

use crate::registry::Registry;
use crate::types::{MetricValue, Options};
use crate::{Error, Result};

/// A metric registered in the registry.
pub trait Metric: Send + Sync {
    /// The identifier as passed to the constructor (with `/`).
    fn id(&self) -> &str;
    /// The Prometheus-compatible, fully labeled identifier.
    fn labeled_id(&self) -> &str;
    /// The labels attached to this metric (including global labels).
    fn labels(&self) -> &BTreeMap<String, String>;
    /// The metric options; they are immutable after registration.
    fn options(&self) -> &Options;
    /// Writes the metric in Prometheus text format, including a trailing newline.
    fn write_prometheus(&self, out: &mut String);
    /// The current value reported through the structured API, if supported.
    fn current_value(&self) -> Option<MetricValue> {
        None
    }
    /// Downcasting support, used by the persistence layer.
    fn as_any(&self) -> &dyn Any;
}

/// Fields shared by every metric type.
#[derive(Debug, Clone)]
pub struct MetricBase {
    identifier: String,
    labels: BTreeMap<String, String>,
    labeled_identifier: String,
    options: Options,
}

impl MetricBase {
    /// Creates a validated metric base and builds its labeled identifier.
    pub fn new(
        id: &str,
        labels: Option<BTreeMap<String, String>>,
        mut options: Options,
        registry: &Registry,
    ) -> Result<Self> {
        let prometheus_id = id.replace('/', "_");
        if !valid_prometheus_name(&prometheus_id) {
            return Err(Error::InvalidOptions(format!(
                "metric name {id:?} must match ^[a-zA-Z_][a-zA-Z0-9_]*$"
            )));
        }
        for label_name in labels.as_ref().unwrap_or(&BTreeMap::new()).keys() {
            if !valid_prometheus_name(label_name) {
                return Err(Error::InvalidOptions(format!(
                    "metric label name {label_name:?} must match ^[a-zA-Z_][a-zA-Z0-9_]*$"
                )));
            }
        }

        options.normalize();
        let mut labels = labels.unwrap_or_default();
        let labeled_identifier = registry.build_labeled_id(&prometheus_id, &mut labels);

        Ok(MetricBase {
            identifier: id.to_string(),
            labels,
            labeled_identifier,
            options,
        })
    }

    pub fn id(&self) -> &str {
        &self.identifier
    }

    pub fn labeled_id(&self) -> &str {
        &self.labeled_identifier
    }

    pub fn labels(&self) -> &BTreeMap<String, String> {
        &self.labels
    }

    pub fn options(&self) -> &Options {
        &self.options
    }
}

/// Checks the Prometheus name requirement without a regex dependency.
pub(crate) fn valid_prometheus_name(name: &str) -> bool {
    let mut chars = name.chars();
    let Some(first) = chars.next() else {
        return false;
    };
    if !(first.is_ascii_alphabetic() || first == '_') {
        return false;
    }
    chars.all(|c| c.is_ascii_alphanumeric() || c == '_')
}
