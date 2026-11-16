//! Histogram metric with VictoriaMetrics-compatible automatic buckets.

use std::any::Any;
use std::collections::BTreeMap;
use std::sync::{Mutex, OnceLock};

use crate::metric::MetricBase;
use crate::types::{format_metric_float, go_exponent_3, go_quote, MetricValue, Options};
use crate::Metric;

const E10_MIN: i32 = -9;
const E10_MAX: i32 = 18;
const BUCKETS_PER_DECIMAL: usize = 18;
const DECIMAL_BUCKETS_COUNT: usize = (E10_MAX - E10_MIN) as usize;
const BUCKETS_COUNT: usize = DECIMAL_BUCKETS_COUNT * BUCKETS_PER_DECIMAL;

fn bucket_multiplier() -> f64 {
    10.0_f64.powf(1.0 / BUCKETS_PER_DECIMAL as f64)
}

fn lower_bucket_range() -> String {
    format!("0...{}", go_exponent_3(10.0_f64.powi(E10_MIN)))
}

fn upper_bucket_range() -> String {
    format!("{}...+Inf", go_exponent_3(10.0_f64.powi(E10_MAX)))
}

fn bucket_ranges() -> &'static Vec<String> {
    static RANGES: OnceLock<Vec<String>> = OnceLock::new();
    RANGES.get_or_init(|| {
        let mut ranges = Vec::with_capacity(BUCKETS_COUNT);
        let mut value = 10.0_f64.powi(E10_MIN);
        let mut start = go_exponent_3(value);
        for _ in 0..BUCKETS_COUNT {
            value *= bucket_multiplier();
            let end = go_exponent_3(value);
            ranges.push(format!("{start}...{end}"));
            start = end;
        }
        ranges
    })
}

#[derive(Debug, Default)]
struct HistogramInner {
    lower: u64,
    upper: u64,
    sum: f64,
    buckets: Vec<Option<[u64; BUCKETS_PER_DECIMAL]>>,
}

/// A histogram with automatically created `vmrange` buckets, matching
/// VictoriaMetrics' `Histogram` output.
#[derive(Debug)]
pub struct Histogram {
    base: MetricBase,
    inner: Mutex<HistogramInner>,
}

impl Histogram {
    pub(crate) fn new(base: MetricBase) -> Self {
        Histogram {
            base,
            inner: Mutex::new(HistogramInner::default()),
        }
    }

    /// Adds a non-negative value to the histogram. NaNs and negatives are ignored.
    pub fn update(&self, value: f64) {
        if value.is_nan() || value < 0.0 {
            return;
        }
        let bucket_index = (value.log10() - E10_MIN as f64) * BUCKETS_PER_DECIMAL as f64;
        let mut inner = self.inner.lock().expect("lock poisoned");
        inner.sum += value;
        if bucket_index < 0.0 {
            inner.lower += 1;
        } else if bucket_index >= BUCKETS_COUNT as f64 {
            inner.upper += 1;
        } else {
            let mut index = bucket_index as usize;
            if bucket_index == index as f64 && index > 0 {
                index -= 1;
            }
            let decimal_index = index / BUCKETS_PER_DECIMAL;
            let offset = index % BUCKETS_PER_DECIMAL;
            if inner.buckets.len() <= decimal_index {
                inner.buckets.resize_with(decimal_index + 1, || None);
            }
            let bucket = inner.buckets[decimal_index].get_or_insert([0; BUCKETS_PER_DECIMAL]);
            bucket[offset] += 1;
        }
    }

    /// Adds the duration since `start` (in seconds), like `UpdateDuration`.
    pub fn update_duration(&self, start: std::time::Instant) {
        self.update(start.elapsed().as_secs_f64());
    }
}

impl Metric for Histogram {
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
        let inner = self.inner.lock().expect("lock poisoned");
        let (name, labels) = match self.base.labeled_id().find('{') {
            Some(index) => (&self.base.labeled_id()[..index], &self.base.labeled_id()[index..]),
            None => (self.base.labeled_id(), ""),
        };
        let mut total = 0u64;

        let write_bucket = |range: &str, count: u64, out: &mut String| {
            let mut label = String::from(labels);
            if label.is_empty() || label == "{}" {
                label = format!("{{vmrange={}}}", go_quote(range));
            } else {
                label.pop(); // remove '}'
                label.push(',');
                label.push_str("vmrange=");
                label.push_str(&go_quote(range));
                label.push('}');
            }
            out.push_str(name);
            out.push_str("_bucket");
            out.push_str(&label);
            out.push(' ');
            out.push_str(&count.to_string());
            out.push('\n');
        };

        if inner.lower > 0 {
            write_bucket(&lower_bucket_range(), inner.lower, out);
            total += inner.lower;
        }
        for (decimal_index, bucket) in inner.buckets.iter().enumerate() {
            let Some(bucket) = bucket else {
                continue;
            };
            for (offset, count) in bucket.iter().enumerate() {
                if *count > 0 {
                    let bucket_index = decimal_index * BUCKETS_PER_DECIMAL + offset;
                    write_bucket(&bucket_ranges()[bucket_index], *count, out);
                    total += *count;
                }
            }
        }
        if inner.upper > 0 {
            write_bucket(&upper_bucket_range(), inner.upper, out);
            total += inner.upper;
        }
        if total == 0 {
            return;
        }
        out.push_str(name);
        out.push_str("_sum");
        out.push_str(labels);
        out.push(' ');
        out.push_str(&format_metric_float(inner.sum));
        out.push('\n');
        out.push_str(name);
        out.push_str("_count");
        out.push_str(labels);
        out.push(' ');
        out.push_str(&total.to_string());
        out.push('\n');
    }

    fn current_value(&self) -> Option<MetricValue> {
        None
    }

    fn as_any(&self) -> &dyn Any {
        self
    }
}
