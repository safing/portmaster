//! Shared value types and Go-compatible formatting helpers.

use std::fmt;
use std::sync::Arc;

use pm_api::{Permission, PERMIT_ANYONE, PERMIT_USER};
use pm_config::ExpertiseLevel;
use serde::Serialize;
use serde_json::Value;

/// Advanced metric settings, port of the Go `metrics.Options` struct.
///
/// The serde field names match the Go struct exactly, because the options are
/// embedded verbatim in the `metrics/list` API response.
#[derive(Debug, Clone, Serialize)]
pub struct Options {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "InternalID")]
    pub internal_id: String,
    #[serde(rename = "AlertLimit")]
    pub alert_limit: f64,
    #[serde(rename = "AlertTimeframe")]
    pub alert_timeframe: f64,
    #[serde(rename = "Permission")]
    pub permission: Permission,
    #[serde(rename = "ExpertiseLevel")]
    pub expertise_level: ExpertiseLevel,
    #[serde(rename = "Persist")]
    pub persist: bool,
}

impl Default for Options {
    fn default() -> Self {
        Options {
            name: String::new(),
            internal_id: String::new(),
            alert_limit: 0.0,
            alert_timeframe: 0.0,
            permission: 0,
            expertise_level: 0,
            persist: false,
        }
    }
}

impl Options {
    /// Applies the Go default: permissions below `PermitAnyone` become
    /// `PermitUser`.
    pub(crate) fn normalize(&mut self) {
        if self.permission < PERMIT_ANYONE {
            self.permission = PERMIT_USER;
        }
    }
}

/// The current value of a metric, as reported by the structured API.
#[derive(Debug, Clone, Copy)]
pub enum MetricValue {
    UInt(u64),
    Float(f64),
}

impl MetricValue {
    /// Converts the value into JSON, matching Go's `encoding/json` output for
    /// `uint64`/`float64` (integral floats are emitted without a decimal part).
    pub fn to_json(self) -> Value {
        match self {
            MetricValue::UInt(value) => Value::from(value),
            MetricValue::Float(value) => {
                let integer = value as i64;
                if integer as f64 == value {
                    Value::from(integer)
                } else {
                    Value::from(value)
                }
            }
        }
    }
}

/// A callback returning a gauge value.
#[derive(Clone)]
pub struct GaugeFn(Arc<dyn Fn() -> f64 + Send + Sync>);

impl GaugeFn {
    pub fn new(function: impl Fn() -> f64 + Send + Sync + 'static) -> Self {
        GaugeFn(Arc::new(function))
    }

    pub fn call(&self) -> f64 {
        (self.0)()
    }
}

impl fmt::Debug for GaugeFn {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("GaugeFn")
    }
}

/// A callback returning a fetching-counter value.
#[derive(Clone)]
pub struct FetchFn(Arc<dyn Fn() -> u64 + Send + Sync>);

impl FetchFn {
    pub fn new(function: impl Fn() -> u64 + Send + Sync + 'static) -> Self {
        FetchFn(Arc::new(function))
    }

    pub fn call(&self) -> u64 {
        (self.0)()
    }
}

impl fmt::Debug for FetchFn {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("FetchFn")
    }
}

/// Quotes a string like Go's `%q` verb, which is used for Prometheus labels.
pub fn go_quote(text: &str) -> String {
    let mut out = String::with_capacity(text.len() + 2);
    out.push('"');
    for character in text.chars() {
        match character {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '\u{7}' => out.push_str("\\a"),
            '\u{8}' => out.push_str("\\b"),
            '\u{c}' => out.push_str("\\f"),
            '\u{b}' => out.push_str("\\v"),
            c if c.is_control() && (c as u32) <= 0xff => {
                out.push_str(&format!("\\x{:02x}", c as u32));
            }
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

/// Formats a float like Go's `strconv.FormatFloat(v, 'g', -1, 64)`.
///
/// VictoriaMetrics uses `%g` for non-integral gauge values and histogram sums.
pub fn go_format_float(value: f64) -> String {
    if value.is_nan() {
        return "NaN".to_string();
    }
    if value.is_infinite() {
        return if value.is_sign_negative() {
            "-Inf".to_string()
        } else {
            "+Inf".to_string()
        };
    }
    if value == 0.0 {
        return if value.is_sign_negative() {
            "-0".to_string()
        } else {
            "0".to_string()
        };
    }

    let negative = value.is_sign_negative();
    let scientific = format!("{:e}", value.abs());
    let (mantissa, exponent_text) = scientific
        .split_once('e')
        .expect("lower-exponent always has an exponent");
    let exponent: i32 = exponent_text
        .parse()
        .expect("lower-exponent exponent is an integer");
    let digits: String = mantissa.chars().filter(|c| *c != '.').collect();
    let integer_digits = mantissa.find('.').unwrap_or(mantissa.len()) as i32;
    let decimal_point = integer_digits + exponent;
    let exponent_g = decimal_point - 1;

    let mut out = String::new();
    if negative {
        out.push('-');
    }
    if !(-4..6).contains(&exponent_g) {
        out.push_str(mantissa);
        out.push('e');
        out.push_str(&format!("{exponent_g:+03}"));
    } else if decimal_point <= 0 {
        out.push_str("0.");
        for _ in 0..-decimal_point {
            out.push('0');
        }
        out.push_str(&digits);
    } else if decimal_point >= digits.len() as i32 {
        out.push_str(&digits);
        for _ in 0..(decimal_point - digits.len() as i32) {
            out.push('0');
        }
    } else {
        let point = decimal_point as usize;
        out.push_str(&digits[..point]);
        out.push('.');
        out.push_str(&digits[point..]);
    }
    out
}

/// Formats a value like Go's `%.3e`, including the signed, two-digit exponent.
pub fn go_exponent_3(value: f64) -> String {
    let formatted = format!("{value:.3e}");
    let (mantissa, exponent_text) = formatted
        .split_once('e')
        .expect("lower-exponent always has an exponent");
    let exponent: i32 = exponent_text
        .parse()
        .expect("lower-exponent exponent is an integer");
    format!("{mantissa}e{exponent:+03}")
}

/// Formats a metric float the way VictoriaMetrics marshals gauges and sums:
/// integral values use integer notation, everything else uses Go's `%g`.
pub fn format_metric_float(value: f64) -> String {
    let integer = value as i64;
    if integer as f64 == value {
        integer.to_string()
    } else {
        go_format_float(value)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn go_float_format_matches_reference() {
        assert_eq!(go_format_float(42.5), "42.5");
        assert_eq!(go_format_float(4.000000000001), "4.000000000001");
        assert_eq!(go_format_float(0.0001), "0.0001");
        assert_eq!(go_format_float(0.00001), "1e-05");
        assert_eq!(go_format_float(1e-9), "1e-09");
        assert_eq!(go_format_float(1e20), "1e+20");
        assert_eq!(go_format_float(1.234567e6), "1.234567e+06");
        assert_eq!(go_format_float(1.0 / 3.0), "0.3333333333333333");
    }

    #[test]
    fn go_quote_escapes_controls() {
        assert_eq!(go_quote("a\nb"), "\"a\\nb\"");
        assert_eq!(go_quote("a\"b\\c"), "\"a\\\"b\\\\c\"");
    }

    #[test]
    fn exponent_3_matches_reference() {
        assert_eq!(go_exponent_3(1e-9), "1.000e-09");
        assert_eq!(go_exponent_3(1e18), "1.000e+18");
    }
}
