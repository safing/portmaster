//! Gauge metric.

use std::any::Any;
use std::collections::BTreeMap;
use std::sync::atomic::{AtomicU64, Ordering};

use crate::metric::{Metric, MetricBase};
use crate::types::{format_metric_float, GaugeFn, MetricValue, Options};

/// A gauge metric, optionally backed by a callback.
#[derive(Debug)]
pub struct Gauge {
    base: MetricBase,
    value_bits: AtomicU64,
    function: Option<GaugeFn>,
}

impl Gauge {
    pub(crate) fn new(base: MetricBase, function: Option<GaugeFn>) -> Self {
        Gauge {
            base,
            value_bits: AtomicU64::new(0),
            function,
        }
    }

    /// Returns the current gauge value.
    pub fn get(&self) -> f64 {
        match &self.function {
            Some(function) => function.call(),
            None => f64::from_bits(self.value_bits.load(Ordering::Relaxed)),
        }
    }

    /// Sets the value of a gauge that was created without a callback.
    pub fn set(&self, value: f64) {
        debug_assert!(
            self.function.is_none(),
            "cannot call Set on gauge created with non-nil callback"
        );
        self.value_bits
            .store(value.to_bits(), Ordering::Relaxed);
    }

    /// Returns the current gauge value, like Go `Gauge.CurrentValue`.
    pub fn current_value(&self) -> f64 {
        self.get()
    }
}

impl Metric for Gauge {
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
        out.push_str(&format_metric_float(self.get()));
        out.push('\n');
    }

    fn current_value(&self) -> Option<MetricValue> {
        Some(MetricValue::Float(self.get()))
    }

    fn as_any(&self) -> &dyn Any {
        self
    }
}
