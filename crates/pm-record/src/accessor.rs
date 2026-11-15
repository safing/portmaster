//! JSON accessor with the subset of gjson/sjson path semantics Portmaster uses:
//! dot-separated keys, `\` escapes the next character, numeric segments index
//! arrays, and a trailing `#` yields the array length.

use serde_json::{Map, Value};

use crate::{Error, Result};

#[derive(Debug, Clone, PartialEq)]
pub struct JsonAccessor {
    value: Value,
}

struct Segment {
    text: String,
    /// Unescaped single `#`.
    hash: bool,
}

fn parse_path(path: &str) -> Vec<Segment> {
    let mut segs = Vec::new();
    let mut cur = String::new();
    let mut escaped_any = false;
    let mut chars = path.chars();
    while let Some(c) = chars.next() {
        match c {
            '\\' => {
                escaped_any = true;
                if let Some(n) = chars.next() {
                    cur.push(n);
                }
            }
            '.' => {
                segs.push(Segment {
                    hash: !escaped_any && cur == "#",
                    text: std::mem::take(&mut cur),
                });
                escaped_any = false;
            }
            _ => cur.push(c),
        }
    }
    segs.push(Segment {
        hash: !escaped_any && cur == "#",
        text: cur,
    });
    segs
}

fn type_name(v: &Value) -> &'static str {
    match v {
        Value::Null => "Null",
        Value::Bool(true) => "True",
        Value::Bool(false) => "False",
        Value::Number(_) => "Number",
        Value::String(_) => "String",
        _ => "JSON",
    }
}

fn index(seg: &str) -> Option<usize> {
    if seg.is_empty() || !seg.bytes().all(|b| b.is_ascii_digit()) {
        return None;
    }
    seg.parse().ok()
}

impl JsonAccessor {
    pub fn new(value: Value) -> Self {
        JsonAccessor { value }
    }

    pub fn from_slice(data: &[u8]) -> Result<Self> {
        Ok(JsonAccessor {
            value: serde_json::from_slice(data)?,
        })
    }

    pub fn value(&self) -> &Value {
        &self.value
    }

    pub fn into_value(self) -> Value {
        self.value
    }

    fn lookup(&self, path: &str) -> Option<std::borrow::Cow<'_, Value>> {
        use std::borrow::Cow;
        if path.is_empty() {
            return None;
        }
        let mut cur = &self.value;
        let segs = parse_path(path);
        let n = segs.len();
        for (i, seg) in segs.into_iter().enumerate() {
            let last = i + 1 == n;
            match cur {
                Value::Object(m) => cur = m.get(&seg.text)?,
                Value::Array(a) if seg.hash => {
                    if !last {
                        return None;
                    }
                    return Some(Cow::Owned(Value::from(a.len())));
                }
                Value::Array(a) => cur = a.get(index(&seg.text)?)?,
                _ => return None,
            }
        }
        Some(Cow::Borrowed(cur))
    }

    pub fn get(&self, path: &str) -> Option<Value> {
        self.lookup(path).map(|v| v.into_owned())
    }

    pub fn get_string(&self, path: &str) -> Option<String> {
        match self.lookup(path)?.as_ref() {
            Value::String(s) => Some(s.clone()),
            _ => None,
        }
    }

    /// Like gjson `Array()`: a non-array value counts as a one-element array and null as empty.
    pub fn get_string_array(&self, path: &str) -> Option<Vec<String>> {
        let v = self.lookup(path)?;
        match v.as_ref() {
            Value::Array(a) => a.iter().map(|e| e.as_str().map(String::from)).collect(),
            Value::Null => Some(Vec::new()),
            Value::String(s) => Some(vec![s.clone()]),
            _ => None,
        }
    }

    pub fn get_int(&self, path: &str) -> Option<i64> {
        match self.lookup(path)?.as_ref() {
            Value::Number(n) => Some(
                n.as_i64()
                    .or_else(|| n.as_u64().map(|u| u as i64))
                    .unwrap_or_else(|| n.as_f64().unwrap_or(0.0) as i64),
            ),
            _ => None,
        }
    }

    pub fn get_float(&self, path: &str) -> Option<f64> {
        match self.lookup(path)?.as_ref() {
            Value::Number(n) => n.as_f64(),
            _ => None,
        }
    }

    pub fn get_bool(&self, path: &str) -> Option<bool> {
        match self.lookup(path)?.as_ref() {
            Value::Bool(b) => Some(*b),
            _ => None,
        }
    }

    pub fn exists(&self, path: &str) -> bool {
        self.lookup(path).is_some()
    }

    /// Sets the value at `path`, creating missing containers. Setting a
    /// string, number, bool or array over an existing value of another type fails.
    pub fn set(&mut self, path: &str, value: Value) -> Result<()> {
        if path.is_empty() {
            return Err(Error::InvalidPath(path.into()));
        }
        if let Some(existing) = self.lookup(path) {
            let ok = match &value {
                Value::String(_) => existing.is_string(),
                Value::Number(_) => existing.is_number(),
                Value::Bool(_) => existing.is_boolean(),
                // Go never type-checks arrays in practice (callers pass
                // []interface{}), so any array value is accepted.
                _ => true,
            };
            if !ok {
                return Err(Error::TypeMismatch {
                    key: path.into(),
                    existing: type_name(&existing),
                    got: type_name(&value),
                });
            }
        }
        let segs = parse_path(path);
        if segs.iter().any(|s| s.hash) {
            return Err(Error::InvalidPath(path.into()));
        }
        let texts: Vec<&str> = segs.iter().map(|s| s.text.as_str()).collect();
        set_at(&mut self.value, &texts, value, path)
    }
}

fn set_at(cur: &mut Value, segs: &[&str], value: Value, path: &str) -> Result<()> {
    let Some((seg, rest)) = segs.split_first() else {
        *cur = value;
        return Ok(());
    };
    // sjson replaces a non-container parent with a fresh container.
    if !cur.is_object() && !cur.is_array() {
        *cur = if index(seg).is_some() {
            Value::Array(Vec::new())
        } else {
            Value::Object(Map::new())
        };
    }
    match cur {
        Value::Object(m) => {
            let child = m.entry((*seg).to_string()).or_insert(Value::Null);
            set_at(child, rest, value, path)
        }
        Value::Array(a) => {
            let i = index(seg).ok_or_else(|| Error::InvalidPath(path.into()))?;
            // Bound padding so a hostile index cannot exhaust memory.
            if i > a.len().saturating_add(65_536) {
                return Err(Error::InvalidPath(path.into()));
            }
            if i >= a.len() {
                a.resize(i + 1, Value::Null);
            }
            set_at(&mut a[i], rest, value, path)
        }
        _ => unreachable!("cur was just made a container"),
    }
}
