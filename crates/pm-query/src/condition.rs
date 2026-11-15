//! Query conditions, ported from `base/database/query/condition-*.go`.
//!
//! String values are byte strings, like in Go: the parser can produce tokens
//! whose last byte was cut off by Go's snippet extractor, and comparisons such
//! as `startswith` must then behave byte-wise to stay compatible.

use std::fmt;

use pm_record::accessor::JsonAccessor;
use regex::Regex;

use crate::gostrconv;
use crate::operators as op;
use crate::Error;

/// A value passed to [`Condition::new`], mirroring Go's `interface{}` argument
/// of `query.Where`.
#[derive(Debug, Clone, PartialEq)]
pub enum Value {
    /// Go `nil`; only meaningful for `exists`.
    Null,
    Int(i64),
    Float(f64),
    /// A Go string. Stored as bytes because Go strings are byte strings.
    Str(Vec<u8>),
    StrList(Vec<Vec<u8>>),
    Bool(bool),
}

impl fmt::Display for Value {
    /// Formats like Go's `%v`.
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Value::Null => f.write_str("<nil>"),
            Value::Int(v) => write!(f, "{v}"),
            Value::Float(v) => f.write_str(&gostrconv::format_float_g(*v)),
            Value::Str(v) => f.write_str(&String::from_utf8_lossy(v)),
            Value::StrList(v) => {
                f.write_str("[")?;
                for (i, item) in v.iter().enumerate() {
                    if i > 0 {
                        f.write_str(" ")?;
                    }
                    f.write_str(&String::from_utf8_lossy(item))?;
                }
                f.write_str("]")
            }
            Value::Bool(v) => write!(f, "{v}"),
        }
    }
}

impl From<&str> for Value {
    fn from(v: &str) -> Self {
        Value::Str(v.as_bytes().to_vec())
    }
}

impl From<String> for Value {
    fn from(v: String) -> Self {
        Value::Str(v.into_bytes())
    }
}

impl From<&String> for Value {
    fn from(v: &String) -> Self {
        Value::Str(v.as_bytes().to_vec())
    }
}

impl From<i64> for Value {
    fn from(v: i64) -> Self {
        Value::Int(v)
    }
}

impl From<i32> for Value {
    fn from(v: i32) -> Self {
        Value::Int(i64::from(v))
    }
}

impl From<i16> for Value {
    fn from(v: i16) -> Self {
        Value::Int(i64::from(v))
    }
}

impl From<i8> for Value {
    fn from(v: i8) -> Self {
        Value::Int(i64::from(v))
    }
}

impl From<u32> for Value {
    fn from(v: u32) -> Self {
        Value::Int(i64::from(v))
    }
}

impl From<u16> for Value {
    fn from(v: u16) -> Self {
        Value::Int(i64::from(v))
    }
}

impl From<u8> for Value {
    fn from(v: u8) -> Self {
        Value::Int(i64::from(v))
    }
}

impl From<usize> for Value {
    fn from(v: usize) -> Self {
        Value::Int(v as i64)
    }
}

impl From<f64> for Value {
    fn from(v: f64) -> Self {
        Value::Float(v)
    }
}

impl From<f32> for Value {
    fn from(v: f32) -> Self {
        Value::Float(f64::from(v))
    }
}

impl From<bool> for Value {
    fn from(v: bool) -> Self {
        Value::Bool(v)
    }
}

impl From<Vec<String>> for Value {
    fn from(v: Vec<String>) -> Self {
        Value::StrList(v.into_iter().map(String::into_bytes).collect())
    }
}

impl From<Vec<&str>> for Value {
    fn from(v: Vec<&str>) -> Self {
        Value::StrList(v.into_iter().map(|s| s.as_bytes().to_vec()).collect())
    }
}

impl From<()> for Value {
    fn from(_: ()) -> Self {
        Value::Null
    }
}

/// A compiled query condition.
#[derive(Debug, Clone)]
pub enum Condition {
    And(Vec<Condition>),
    Or(Vec<Condition>),
    Not(Box<Condition>),
    Exists {
        key: Vec<u8>,
        operator: u8,
    },
    Int {
        key: Vec<u8>,
        operator: u8,
        value: i64,
    },
    Float {
        key: Vec<u8>,
        operator: u8,
        value: f64,
    },
    String {
        key: Vec<u8>,
        operator: u8,
        value: Vec<u8>,
    },
    StringSlice {
        key: Vec<u8>,
        operator: u8,
        value: Vec<Vec<u8>>,
    },
    Bool {
        key: Vec<u8>,
        operator: u8,
        value: bool,
    },
    Regex {
        key: Vec<u8>,
        operator: u8,
        regex: Regex,
    },
    /// A condition that failed to build; `check` returns the message as error.
    Error {
        message: String,
    },
}

impl PartialEq for Condition {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (Condition::And(a), Condition::And(b)) | (Condition::Or(a), Condition::Or(b)) => a == b,
            (Condition::Not(a), Condition::Not(b)) => a == b,
            (
                Condition::Exists { key: k1, operator: o1 },
                Condition::Exists { key: k2, operator: o2 },
            ) => k1 == k2 && o1 == o2,
            (
                Condition::Int {
                    key: k1,
                    operator: o1,
                    value: v1,
                },
                Condition::Int {
                    key: k2,
                    operator: o2,
                    value: v2,
                },
            ) => k1 == k2 && o1 == o2 && v1 == v2,
            (
                Condition::Float {
                    key: k1,
                    operator: o1,
                    value: v1,
                },
                Condition::Float {
                    key: k2,
                    operator: o2,
                    value: v2,
                },
            ) => k1 == k2 && o1 == o2 && v1 == v2,
            (
                Condition::String {
                    key: k1,
                    operator: o1,
                    value: v1,
                },
                Condition::String {
                    key: k2,
                    operator: o2,
                    value: v2,
                },
            ) => k1 == k2 && o1 == o2 && v1 == v2,
            (
                Condition::StringSlice {
                    key: k1,
                    operator: o1,
                    value: v1,
                },
                Condition::StringSlice {
                    key: k2,
                    operator: o2,
                    value: v2,
                },
            ) => k1 == k2 && o1 == o2 && v1 == v2,
            (
                Condition::Bool {
                    key: k1,
                    operator: o1,
                    value: v1,
                },
                Condition::Bool {
                    key: k2,
                    operator: o2,
                    value: v2,
                },
            ) => k1 == k2 && o1 == o2 && v1 == v2,
            (
                Condition::Regex {
                    key: k1,
                    operator: o1,
                    regex: r1,
                },
                Condition::Regex {
                    key: k2,
                    operator: o2,
                    regex: r2,
                },
            ) => k1 == k2 && o1 == o2 && r1.as_str() == r2.as_str(),
            (Condition::Error { message: m1 }, Condition::Error { message: m2 }) => m1 == m2,
            _ => false,
        }
    }
}

impl Condition {
    /// Builds a condition like Go `query.Where`.
    pub fn new(key: &str, operator: u8, value: impl Into<Value>) -> Condition {
        Condition::new_raw(key.as_bytes(), operator, value.into())
    }

    /// Builds a condition from a raw (possibly invalid UTF-8) Go-style key.
    pub(crate) fn new_raw(key: &[u8], operator: u8, value: Value) -> Condition {
        match operator {
            op::EQUALS
            | op::GREATER_THAN
            | op::GREATER_THAN_OR_EQUAL
            | op::LESS_THAN
            | op::LESS_THAN_OR_EQUAL => int_condition(key, operator, value),
            op::FLOAT_EQUALS
            | op::FLOAT_GREATER_THAN
            | op::FLOAT_GREATER_THAN_OR_EQUAL
            | op::FLOAT_LESS_THAN
            | op::FLOAT_LESS_THAN_OR_EQUAL => float_condition(key, operator, value),
            op::SAME_AS | op::CONTAINS | op::STARTS_WITH | op::ENDS_WITH => {
                string_condition(key, operator, value)
            }
            op::IN => string_slice_condition(key, operator, value),
            op::MATCHES => regex_condition(key, operator, value),
            op::IS => bool_condition(key, operator, value),
            op::EXISTS => Condition::Exists {
                key: key.to_vec(),
                operator,
            },
            _ => Condition::Error {
                message: format!("no operator with ID {operator}"),
            },
        }
    }

    /// Combines conditions with a logical _AND_, like Go `query.And`.
    pub fn and(conditions: Vec<Condition>) -> Condition {
        Condition::And(conditions)
    }

    /// Combines conditions with a logical _OR_, like Go `query.Or`.
    pub fn or(conditions: Vec<Condition>) -> Condition {
        Condition::Or(conditions)
    }

    /// Negates a condition, like Go `query.Not`.
    #[allow(clippy::should_implement_trait)] // named after the Go API
    pub fn not(condition: Condition) -> Condition {
        Condition::Not(Box::new(condition))
    }

    /// Checks the condition for build errors.
    pub fn check(&self) -> Result<(), Error> {
        match self {
            Condition::And(conditions) | Condition::Or(conditions) => {
                for condition in conditions {
                    condition.check()?;
                }
                Ok(())
            }
            Condition::Not(condition) => condition.check(),
            Condition::Error { message } => Err(Error::Condition(message.clone())),
            _ => Ok(()),
        }
    }

    /// Evaluates the condition against a JSON accessor.
    pub fn complies(&self, acc: &JsonAccessor) -> bool {
        match self {
            Condition::And(conditions) => conditions.iter().all(|c| c.complies(acc)),
            Condition::Or(conditions) => conditions.iter().any(|c| c.complies(acc)),
            Condition::Not(condition) => !condition.complies(acc),
            Condition::Exists { key, .. } => acc.exists(&key_str(key)),
            Condition::Int {
                key,
                operator,
                value,
            } => {
                let Some(comp) = acc.get_int(&key_str(key)) else {
                    return false;
                };
                match *operator {
                    op::EQUALS => comp == *value,
                    op::GREATER_THAN => comp > *value,
                    op::GREATER_THAN_OR_EQUAL => comp >= *value,
                    op::LESS_THAN => comp < *value,
                    op::LESS_THAN_OR_EQUAL => comp <= *value,
                    _ => false,
                }
            }
            Condition::Float {
                key,
                operator,
                value,
            } => {
                let Some(comp) = acc.get_float(&key_str(key)) else {
                    return false;
                };
                match *operator {
                    op::FLOAT_EQUALS => comp == *value,
                    op::FLOAT_GREATER_THAN => comp > *value,
                    op::FLOAT_GREATER_THAN_OR_EQUAL => comp >= *value,
                    op::FLOAT_LESS_THAN => comp < *value,
                    op::FLOAT_LESS_THAN_OR_EQUAL => comp <= *value,
                    _ => false,
                }
            }
            Condition::String {
                key,
                operator,
                value,
            } => {
                let Some(comp) = acc.get_string(&key_str(key)) else {
                    return false;
                };
                let comp = comp.as_bytes();
                match *operator {
                    op::SAME_AS => comp == value.as_slice(),
                    op::CONTAINS => contains(comp, value),
                    op::STARTS_WITH => comp.starts_with(value),
                    op::ENDS_WITH => comp.ends_with(value),
                    _ => false,
                }
            }
            Condition::StringSlice {
                key,
                operator,
                value,
            } => {
                let Some(comp) = acc.get_string(&key_str(key)) else {
                    return false;
                };
                match *operator {
                    op::IN => value.iter().any(|v| v.as_slice() == comp.as_bytes()),
                    _ => false,
                }
            }
            Condition::Bool {
                key,
                operator,
                value,
            } => {
                let Some(comp) = acc.get_bool(&key_str(key)) else {
                    return false;
                };
                match *operator {
                    op::IS => comp == *value,
                    _ => false,
                }
            }
            Condition::Regex {
                key,
                operator,
                regex,
            } => {
                let Some(comp) = acc.get_string(&key_str(key)) else {
                    return false;
                };
                match *operator {
                    op::MATCHES => regex.is_match(&comp),
                    _ => false,
                }
            }
            Condition::Error { .. } => false,
        }
    }
}

impl fmt::Display for Condition {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let mut out = Vec::new();
        self.fmt_bytes(&mut out);
        f.write_str(&String::from_utf8_lossy(&out))
    }
}

impl Condition {
    /// Writes the Go `string()` representation.
    pub(crate) fn fmt_bytes(&self, out: &mut Vec<u8>) {
        match self {
            Condition::And(conditions) => group(out, conditions, b" and "),
            Condition::Or(conditions) => group(out, conditions, b" or "),
            Condition::Not(condition) => {
                let mut inner = Vec::new();
                condition.fmt_bytes(&mut inner);
                if inner.first() == Some(&b'(') {
                    out.extend_from_slice(b"not ");
                    out.extend_from_slice(&inner);
                } else if let Some(space) = inner.iter().position(|&b| b == b' ') {
                    out.extend_from_slice(&inner[..space]);
                    out.extend_from_slice(b" not ");
                    out.extend_from_slice(&inner[space + 1..]);
                } else {
                    out.extend_from_slice(&inner);
                    out.extend_from_slice(b" not");
                }
            }
            Condition::Exists { key, operator } => {
                escape_into(out, key);
                out.push(b' ');
                out.extend_from_slice(op::name(*operator).as_bytes());
            }
            Condition::Int {
                key,
                operator,
                value,
            } => {
                escape_into(out, key);
                out.push(b' ');
                out.extend_from_slice(op::name(*operator).as_bytes());
                out.push(b' ');
                out.extend_from_slice(value.to_string().as_bytes());
            }
            Condition::Float {
                key,
                operator,
                value,
            } => {
                escape_into(out, key);
                out.push(b' ');
                out.extend_from_slice(op::name(*operator).as_bytes());
                out.push(b' ');
                out.extend_from_slice(gostrconv::format_float_g(*value).as_bytes());
            }
            Condition::String {
                key,
                operator,
                value,
            } => {
                escape_into(out, key);
                out.push(b' ');
                out.extend_from_slice(op::name(*operator).as_bytes());
                out.push(b' ');
                escape_into(out, value);
            }
            Condition::StringSlice {
                key,
                operator,
                value,
            } => {
                escape_into(out, key);
                out.push(b' ');
                out.extend_from_slice(op::name(*operator).as_bytes());
                out.push(b' ');
                let mut joined = Vec::new();
                for (i, item) in value.iter().enumerate() {
                    if i > 0 {
                        joined.push(b',');
                    }
                    joined.extend_from_slice(item);
                }
                escape_into(out, &joined);
            }
            Condition::Bool {
                key,
                operator,
                value,
            } => {
                escape_into(out, key);
                out.push(b' ');
                out.extend_from_slice(op::name(*operator).as_bytes());
                out.push(b' ');
                out.extend_from_slice(if *value { b"true" } else { b"false" });
            }
            Condition::Regex {
                key,
                operator,
                regex,
            } => {
                escape_into(out, key);
                out.push(b' ');
                out.extend_from_slice(op::name(*operator).as_bytes());
                out.push(b' ');
                escape_into(out, regex.as_str().as_bytes());
            }
            Condition::Error { .. } => out.extend_from_slice(b"[ERROR]"),
        }
    }
}

fn group(out: &mut Vec<u8>, conditions: &[Condition], separator: &[u8]) {
    out.push(b'(');
    for (i, condition) in conditions.iter().enumerate() {
        if i > 0 {
            out.extend_from_slice(separator);
        }
        condition.fmt_bytes(out);
    }
    out.push(b')');
}

fn key_str(key: &[u8]) -> std::borrow::Cow<'_, str> {
    String::from_utf8_lossy(key)
}

fn contains(haystack: &[u8], needle: &[u8]) -> bool {
    if needle.is_empty() {
        return true;
    }
    haystack.windows(needle.len()).any(|w| w == needle)
}

/// Go `escapeString`: quote if the token contains characters that need it.
fn escape_into(out: &mut Vec<u8>, token: &[u8]) {
    let needs_quotes = token
        .iter()
        .any(|b| matches!(b, b'(' | b')' | b'"' | b'\\' | b'\t' | b'\r' | b'\n' | b' '));
    if !needs_quotes {
        out.extend_from_slice(token);
        return;
    }
    out.push(b'"');
    for &b in token {
        if b == b'"' {
            out.extend_from_slice(b"\\\"");
        } else {
            out.push(b);
        }
    }
    out.push(b'"');
}

fn int_condition(key: &[u8], operator: u8, value: Value) -> Condition {
    let value = match &value {
        Value::Int(v) => *v,
        Value::Str(s) => match gostrconv::parse_int(s, 64) {
            Ok(v) => v,
            Err(err) => {
                return Condition::Error {
                    message: format!(
                        "could not parse {} to int64: {} (hint: use \"sameas\" to compare strings)",
                        String::from_utf8_lossy(s),
                        err
                    ),
                }
            }
        },
        other => {
            return Condition::Error {
                message: format!("incompatible value {other} for int64"),
            }
        }
    };
    Condition::Int {
        key: key.to_vec(),
        operator,
        value,
    }
}

fn float_condition(key: &[u8], operator: u8, value: Value) -> Condition {
    let value = match &value {
        Value::Int(v) => *v as f64,
        Value::Float(v) => *v,
        Value::Str(s) => match gostrconv::parse_float(s) {
            Ok(v) => v,
            Err(err) => {
                return Condition::Error {
                    message: format!(
                        "could not parse {} to float64: {}",
                        String::from_utf8_lossy(s),
                        err
                    ),
                }
            }
        },
        other => {
            return Condition::Error {
                message: format!("incompatible value {other} for float64"),
            }
        }
    };
    Condition::Float {
        key: key.to_vec(),
        operator,
        value,
    }
}

fn string_condition(key: &[u8], operator: u8, value: Value) -> Condition {
    match value {
        Value::Str(value) => Condition::String {
            key: key.to_vec(),
            operator,
            value,
        },
        other => Condition::Error {
            message: format!("incompatible value {other} for string"),
        },
    }
}

fn string_slice_condition(key: &[u8], operator: u8, value: Value) -> Condition {
    match value {
        Value::Str(value) => {
            let parts: Vec<Vec<u8>> = value.split(|&b| b == b',').map(<[u8]>::to_vec).collect();
            if parts.len() < 2 {
                return Condition::Error {
                    message: format!(
                        "could not parse \"{}\" to []string",
                        String::from_utf8_lossy(&value)
                    ),
                };
            }
            Condition::StringSlice {
                key: key.to_vec(),
                operator,
                value: parts,
            }
        }
        Value::StrList(value) => Condition::StringSlice {
            key: key.to_vec(),
            operator,
            value,
        },
        other => Condition::Error {
            message: format!(
                "could not parse \"incompatible value {other} for []string\" to []string"
            ),
        },
    }
}

fn regex_condition(key: &[u8], operator: u8, value: Value) -> Condition {
    match value {
        Value::Str(value) => {
            let pattern = String::from_utf8_lossy(&value);
            match Regex::new(&pattern) {
                Ok(regex) => Condition::Regex {
                    key: key.to_vec(),
                    operator,
                    regex,
                },
                Err(err) => Condition::Error {
                    message: format!("could not compile regex \"{pattern}\": {err}"),
                },
            }
        }
        other => Condition::Error {
            message: format!("incompatible value {other} for string"),
        },
    }
}

fn bool_condition(key: &[u8], operator: u8, value: Value) -> Condition {
    let value = match &value {
        Value::Bool(v) => *v,
        Value::Str(s) => match gostrconv::parse_bool(s) {
            Ok(v) => v,
            Err(err) => {
                return Condition::Error {
                    message: format!(
                        "could not parse {} to bool: {}",
                        gostrconv::quote(s),
                        err
                    ),
                }
            }
        },
        other => {
            // Go reports "int64" here; kept for compatible error strings.
            return Condition::Error {
                message: format!("incompatible value {other} for int64"),
            }
        }
    };
    Condition::Bool {
        key: key.to_vec(),
        operator,
        value,
    }
}
