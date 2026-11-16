//! Fetching counter metric.

use std::any::Any;
use std::collections::BTreeMap;
use std::sync::atomic::{AtomicU64, Ordering};

use crate::metric::{Metric, MetricBase};
use crate::types::{FetchFn, MetricValue, Options};

/// A counter whose value is obtained from a callback on every read.
#[derive(Debug)]
pub struct FetchingCounter {
    base: MetricBase,
    counter: AtomicU64,
    fetch: FetchFn,
}

impl FetchingCounter {
    pub(crate) fn new(base: MetricBase, fetch: FetchFn) -> Self {
        FetchingCounter {
            base,
            counter: AtomicU64::new(0),
            fetch,
        }
    }

    /// Returns the current counter value, like Go `FetchingCounter.CurrentValue`.
    pub fn current_value(&self) -> u64 {
        self.fetch.call()
    }
}

impl Metric for FetchingCounter {
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
        let value = self.fetch.call();
        self.counter.store(value, Ordering::Relaxed);
        out.push_str(self.base.labeled_id());
        out.push(' ');
        out.push_str(&value.to_string());
        out.push('\n');
    }

    fn current_value(&self) -> Option<MetricValue> {
        Some(MetricValue::UInt(self.fetch.call()))
    }

    fn as_any(&self) -> &dyn Any {
        self
    }
}
