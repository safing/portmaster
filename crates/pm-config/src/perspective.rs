//! Perspectives: read-only views on configuration data without changing it.

use std::collections::BTreeMap;
use std::sync::{Arc, Weak};

use serde_json::{Map, Value};

use crate::option::{CachedValue, ReleaseLevel, OPT_TYPE_STRING, OPT_TYPE_STRING_ARRAY};
use crate::{persistence, Config, Error};

/// A view on configuration data, used for per-app profiles.
pub struct Perspective {
    config: Weak<Config>,
    release_level: ReleaseLevel,
    values: BTreeMap<String, CachedValue>,
    option_levels: BTreeMap<String, ReleaseLevel>,
}

impl Perspective {
    /// Parses a nested config and validates the values of registered options.
    pub(crate) fn new(config: &Arc<Config>, nested: &Value) -> (Perspective, Option<Error>) {
        let flattened = match nested {
            Value::Object(map) => persistence::flatten(map),
            _ => Map::new(),
        };

        let mut perspective = Perspective {
            config: Arc::downgrade(config),
            release_level: config.release_level(),
            values: BTreeMap::new(),
            option_levels: BTreeMap::new(),
        };
        let mut errors = Vec::new();

        for option in config.options_snapshot() {
            let Some(value) = flattened.get(option.key()) else {
                continue;
            };
            match option.validate(value) {
                Ok(cached) => {
                    perspective.values.insert(option.key().to_string(), cached);
                    perspective
                        .option_levels
                        .insert(option.key().to_string(), option.definition().release_level);
                }
                Err(err) => errors.push(err),
            }
        }

        let error = if errors.is_empty() {
            None
        } else {
            Some(Error::InvalidOption(format!(
                "encountered {} errors, first was: {}",
                errors.len(),
                errors[0]
            )))
        };
        (perspective, error)
    }

    fn current_release_level(&self) -> ReleaseLevel {
        self.config
            .upgrade()
            .map(|config| config.release_level())
            .unwrap_or(self.release_level)
    }

    fn cache_for(&self, name: &str) -> Option<&CachedValue> {
        let level = *self.option_levels.get(name)?;
        if level > self.current_release_level() {
            return None;
        }
        self.values.get(name)
    }

    /// Returns whether the option is set in this perspective.
    pub fn has(&self, name: &str) -> bool {
        self.cache_for(name).is_some()
    }

    pub fn get_string(&self, name: &str) -> Option<String> {
        match self.cache_for(name)? {
            CachedValue::String(value) => Some(value.clone()),
            _ => None,
        }
    }

    pub fn get_string_array(&self, name: &str) -> Option<Vec<String>> {
        match self.cache_for(name)? {
            CachedValue::StringArray(value) => Some(value.clone()),
            _ => None,
        }
    }

    pub fn get_int(&self, name: &str) -> Option<i64> {
        match self.cache_for(name)? {
            CachedValue::Int(value) => Some(*value),
            _ => None,
        }
    }

    pub fn get_bool(&self, name: &str) -> Option<bool> {
        match self.cache_for(name)? {
            CachedValue::Bool(value) => Some(*value),
            _ => None,
        }
    }

    /// Returns a typed getter, mirroring the type checks of the Go methods.
    pub fn get_typed(&self, name: &str, expected_type: u8) -> Option<CachedValue> {
        let value = self.cache_for(name)?;
        let matches = matches!(
            (expected_type, value),
            (OPT_TYPE_STRING, CachedValue::String(_))
                | (OPT_TYPE_STRING_ARRAY, CachedValue::StringArray(_))
                | (crate::OPT_TYPE_INT, CachedValue::Int(_))
                | (crate::OPT_TYPE_BOOL, CachedValue::Bool(_))
                | (crate::OPT_TYPE_ANY, _)
        );
        matches.then(|| value.clone())
    }
}
