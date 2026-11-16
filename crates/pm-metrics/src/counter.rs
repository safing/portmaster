//! Counter metric.

use std::any::Any;
use std::collections::BTreeMap;
use std::sync::atomic::{AtomicU64, Ordering};

use crate::metric::{Metric, MetricBase};
use crate::types::{MetricValue, Options};

/// A monotonically increasing counter metric.
#[derive(Debug)]
pub struct Counter {
    base: MetricBase,
    value: AtomicU64,
}

impl Counter {
    pub(crate) fn new(base: MetricBase) -> Self {
        Counter {
            base,
            value: AtomicU64::new(0),
        }
    }

    pub fn inc(&self) {
        self.value.fetch_add(1, Ordering::Relaxed);
    }

    pub fn add(&self, amount: u64) {
        self.value.fetch_add(amount, Ordering::Relaxed);
    }

    pub fn set(&self, value: u64) {
        self.value.store(value, Ordering::Relaxed);
    }

    pub fn get(&self) -> u64 {
        self.value.load(Ordering::Relaxed)
    }

    /// Returns the current counter value, like Go `Counter.CurrentValue`.
    pub fn current_value(&self) -> u64 {
        self.get()
    }

    pub(crate) fn base(&self) -> &MetricBase {
        &self.base
    }
}

impl Metric for Counter {
    fn id(&self) -> &str {
        self.base.id()
    }

    fn labeled_id(&self) -> &str {
        self.base.labeled_id()
    }

    fn labels(&self) -> &BTreeMap<String, String> {
        self.base.labels()
    }

    fn options(&self) -> &Options {
        self.base.options()
    }

    fn write_prometheus(&self, out: &mut String) {
        out.push_str(self.base.labeled_id());
        out.push(' ');
        out.push_str(&self.get().to_string());
        out.push('\n');
    }

    fn current_value(&self) -> Option<MetricValue> {
        Some(MetricValue::UInt(self.get()))
    }

    fn as_any(&self) -> &dyn Any {
        self
    }
}
