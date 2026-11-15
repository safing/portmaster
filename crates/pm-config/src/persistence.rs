//! Persistence helpers: flattening, expanding and Go-compatible JSON output.

use std::collections::BTreeMap;

use serde_json::{Map, Number, Value};

/// Parses and flattens a hierarchical JSON object, like Go `JSONToMap`.
pub fn json_to_map(data: &[u8]) -> crate::Result<Map<String, Value>> {
    let value: Value = serde_json::from_slice(data)?;
    let Value::Object(object) = value else {
        return Err(crate::Error::InvalidJsonConfig(
            "config file is not a JSON object".to_string(),
        ));
    };
    Ok(flatten(&object))
}

/// Flattens a hierarchical config, joining keys with `/`.
pub fn flatten(config: &Map<String, Value>) -> Map<String, Value> {
    let mut flattened = Map::new();
    flatten_into(&mut flattened, config, "");
    // Go serializes maps with sorted keys; IndexMap keeps insertion order.
    let mut sorted: Vec<(String, Value)> = flattened.into_iter().collect();
    sorted.sort_by(|a, b| a.0.cmp(&b.0));
    sorted.into_iter().collect()
}

fn flatten_into(root: &mut Map<String, Value>, sub: &Map<String, Value>, sub_key: &str) {
    for (key, entry) in sub {
        let full_key = if sub_key.is_empty() {
            key.clone()
        } else {
            format!("{sub_key}/{key}")
        };
        match entry {
            Value::Object(next) => flatten_into(root, next, &full_key),
            _ => {
                root.insert(full_key, normalize(entry));
            }
        }
    }
}

/// Converts integral floats to integers, like Go's `float64` re-marshalling.
fn normalize(value: &Value) -> Value {
    let Value::Number(number) = value else {
        return value.clone();
    };
    if number.is_i64() || number.is_u64() {
        return value.clone();
    }
    if let Some(float) = number.as_f64() {
        if float.fract() == 0.0 && float >= i64::MIN as f64 && float <= i64::MAX as f64 {
            return Value::Number(Number::from(float as i64));
        }
    }
    value.clone()
}

/// Expands a flattened config into a hierarchical one, like Go `Expand`.
pub fn expand(flattened: &Map<String, Value>) -> Map<String, Value> {
    let mut config = Map::new();
    for (key, value) in flattened {
        put_value_into_hierarchical_config(&mut config, key, value.clone());
    }
    config
}

/// Inserts a flattened key into a hierarchical config.
pub fn put_value_into_hierarchical_config(
    config: &mut Map<String, Value>,
    key: &str,
    value: Value,
) {
    let parts: Vec<&str> = key.split('/').collect();
    let mut sub_map = config;
    for part in &parts[..parts.len().saturating_sub(1)] {
        let next = sub_map
            .entry((*part).to_string())
            .or_insert_with(|| Value::Object(Map::new()));
        if !next.is_object() {
            *next = Value::Object(Map::new());
        }
        sub_map = next.as_object_mut().expect("just made an object");
    }
    sub_map.insert(parts[parts.len() - 1].to_string(), value);
}

/// Sorts object keys recursively, matching Go's map serialization.
fn sort_keys(value: &Value) -> Value {
    match value {
        Value::Object(object) => {
            let mut sorted: BTreeMap<&String, Value> = BTreeMap::new();
            for (key, entry) in object {
                sorted.insert(key, sort_keys(entry));
            }
            Value::Object(
                sorted
                    .into_iter()
                    .map(|(key, value)| (key.clone(), value))
                    .collect(),
            )
        }
        Value::Array(entries) => Value::Array(entries.iter().map(sort_keys).collect()),
        other => other.clone(),
    }
}

/// Expands a flattened config and marshals it like Go `json.MarshalIndent`
/// with two spaces, including Go's HTML escaping.
pub fn map_to_json(flattened: &Map<String, Value>) -> crate::Result<Vec<u8>> {
    let config = sort_keys(&Value::Object(expand(flattened)));
    Ok(pm_dsd::gojson::to_vec_indent(&config, "", "  ")?)
}
