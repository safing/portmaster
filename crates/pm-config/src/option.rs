//! Configuration options: types, definitions, validation and export.

use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use std::collections::BTreeMap;
use std::fmt;
use std::sync::Arc;

/// Type of a configuration value. `0` means "any" and is not allowed for
/// registered options.
pub type OptionType = u8;
pub const OPT_TYPE_ANY: OptionType = 0;
pub const OPT_TYPE_STRING: OptionType = 1;
pub const OPT_TYPE_STRING_ARRAY: OptionType = 2;
pub const OPT_TYPE_INT: OptionType = 3;
pub const OPT_TYPE_BOOL: OptionType = 4;

/// Returns the Go name of the option type.
pub fn type_name(option_type: OptionType) -> &'static str {
    match option_type {
        OPT_TYPE_ANY => "any",
        OPT_TYPE_STRING => "string",
        OPT_TYPE_STRING_ARRAY => "[]string",
        OPT_TYPE_INT => "int",
        OPT_TYPE_BOOL => "bool",
        _ => "unknown",
    }
}

/// How much expertise a user needs to understand an option.
pub type ExpertiseLevel = u8;
pub const EXPERTISE_LEVEL_USER: ExpertiseLevel = 0;
pub const EXPERTISE_LEVEL_EXPERT: ExpertiseLevel = 1;
pub const EXPERTISE_LEVEL_DEVELOPER: ExpertiseLevel = 2;

/// Stability of an option.
pub type ReleaseLevel = u8;
pub const RELEASE_LEVEL_STABLE: ReleaseLevel = 0;
pub const RELEASE_LEVEL_BETA: ReleaseLevel = 1;
pub const RELEASE_LEVEL_EXPERIMENTAL: ReleaseLevel = 2;

/// Well-known annotation keys.
pub const DISPLAY_HINT_ANNOTATION: &str = "safing/portbase:ui:display-hint";
pub const DISPLAY_ORDER_ANNOTATION: &str = "safing/portbase:ui:order";
pub const UNIT_ANNOTATION: &str = "safing/portbase:ui:unit";
pub const CATEGORY_ANNOTATION: &str = "safing/portbase:ui:category";
pub const SUBSYSTEM_ANNOTATION: &str = "safing/portbase:module:subsystem";
pub const STACKABLE_ANNOTATION: &str = "safing/portbase:options:stackable";
pub const RESTART_PENDING_ANNOTATION: &str = "safing/portbase:options:restart-pending";
pub const QUICK_SETTINGS_ANNOTATION: &str = "safing/portbase:ui:quick-setting";
pub const REQUIRES_ANNOTATION: &str = "safing/portbase:config:requires";
pub const REQUIRES_FEATURE_ID_ANNOTATION: &str = "safing/portmaster:ui:config:requires-feature";
pub const SETTABLE_PER_APP_ANNOTATION: &str = "safing/portmaster:settable-per-app";
pub const REQUIRES_UI_RELOAD_ANNOTATION: &str = "safing/portmaster:ui:requires-reload";

/// Values of the display-hint annotation.
pub const DISPLAY_HINT_ONE_OF: &str = "one-of";
pub const DISPLAY_HINT_ORDERED: &str = "ordered";
pub const DISPLAY_HINT_FILE_PICKER: &str = "file-picker";

/// Additional validation of a value that already passed the type, regex and
/// possible-value checks, like Go `Option.ValidationFunc`. It receives the
/// validated value as JSON (string, string array, int or bool).
#[derive(Clone)]
pub struct ValidationFunc(Arc<ValidationFn>);

type ValidationFn = dyn Fn(&Value) -> Result<(), String> + Send + Sync;

impl ValidationFunc {
    pub fn new(func: impl Fn(&Value) -> Result<(), String> + Send + Sync + 'static) -> Self {
        ValidationFunc(Arc::new(func))
    }

    pub fn call(&self, value: &Value) -> Result<(), String> {
        (self.0)(value)
    }
}

impl fmt::Debug for ValidationFunc {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("ValidationFunc")
    }
}

impl PartialEq for ValidationFunc {
    fn eq(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.0, &other.0)
    }
}

/// Converts a raw value of an option to the current format before it is
/// validated, like Go `config.MigrationFunc`.
#[derive(Clone)]
pub struct MigrationFunc(Arc<MigrationFn>);

type MigrationFn = dyn Fn(&ConfigOption, &Value) -> Value + Send + Sync;

impl MigrationFunc {
    pub fn new(func: impl Fn(&ConfigOption, &Value) -> Value + Send + Sync + 'static) -> Self {
        MigrationFunc(Arc::new(func))
    }

    pub fn call(&self, option: &ConfigOption, value: &Value) -> Value {
        (self.0)(option, value)
    }
}

impl fmt::Debug for MigrationFunc {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("MigrationFunc")
    }
}

impl PartialEq for MigrationFunc {
    fn eq(&self, other: &Self) -> bool {
        Arc::ptr_eq(&self.0, &other.0)
    }
}

/// Migrates a legacy security level (int) value to a boolean, like Go
/// `status.MigrateSecurityLevelToBoolean`.
///
/// The level is truncated to 8 bits; the option is enabled if the "normal"
/// bit (1) is set. Non-numeric values and non-boolean options are unchanged.
pub fn migrate_security_level_to_boolean(option: &ConfigOption, value: &Value) -> Value {
    if option.opt_type != OPT_TYPE_BOOL {
        return value.clone();
    }
    let Value::Number(number) = value else {
        return value.clone();
    };
    let level = if let Some(v) = number.as_i64() {
        v as u8
    } else if let Some(v) = number.as_u64() {
        v as u8
    } else if let Some(v) = number.as_f64() {
        v as i64 as u8
    } else {
        return value.clone();
    };
    Value::Bool(level & 1 > 0)
}

/// A value that is allowed for an option.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct PossibleValue {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Description", default)]
    pub description: String,
    #[serde(rename = "Value")]
    pub value: Value,
}

/// Definition of a configuration option, serialized like Go `config.Option`.
///
/// Field order and JSON names match Go, and `serde_json` keeps that order when
/// the value is serialized. Runtime values are not part of the definition.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ConfigOption {
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Key")]
    pub key: String,
    #[serde(rename = "Description")]
    pub description: String,
    #[serde(rename = "Help", default)]
    pub help: String,
    #[serde(rename = "Sensitive", default)]
    pub sensitive: bool,
    #[serde(rename = "OptType")]
    pub opt_type: OptionType,
    #[serde(rename = "ExpertiseLevel", default)]
    pub expertise_level: ExpertiseLevel,
    #[serde(rename = "ReleaseLevel", default)]
    pub release_level: ReleaseLevel,
    #[serde(rename = "RequiresRestart", default)]
    pub requires_restart: bool,
    #[serde(rename = "DefaultValue")]
    pub default_value: Value,
    #[serde(rename = "ValidationRegex", default)]
    pub validation_regex: String,
    #[serde(
        rename = "PossibleValues",
        default,
        skip_serializing_if = "Vec::is_empty"
    )]
    pub possible_values: Vec<PossibleValue>,
    #[serde(rename = "Annotations")]
    pub annotations: Option<Map<String, Value>>,
    /// Not part of the exported definition.
    #[serde(skip)]
    pub validation_func: Option<ValidationFunc>,
    /// Not part of the exported definition.
    #[serde(skip)]
    pub migrations: Vec<MigrationFunc>,
}

impl Default for ConfigOption {
    fn default() -> Self {
        ConfigOption {
            name: String::new(),
            key: String::new(),
            description: String::new(),
            help: String::new(),
            sensitive: false,
            opt_type: OPT_TYPE_ANY,
            expertise_level: EXPERTISE_LEVEL_USER,
            release_level: RELEASE_LEVEL_STABLE,
            requires_restart: false,
            default_value: Value::Null,
            validation_regex: String::new(),
            possible_values: Vec::new(),
            annotations: None,
            validation_func: None,
            migrations: Vec::new(),
        }
    }
}

impl ConfigOption {
    /// Creates an option definition with the required fields.
    pub fn new(
        name: &str,
        key: &str,
        description: &str,
        opt_type: OptionType,
        default_value: Value,
    ) -> ConfigOption {
        ConfigOption {
            name: name.to_string(),
            key: key.to_string(),
            description: description.to_string(),
            opt_type,
            default_value,
            ..ConfigOption::default()
        }
    }

    /// Sets the release level.
    #[must_use]
    pub fn with_release_level(mut self, level: ReleaseLevel) -> ConfigOption {
        self.release_level = level;
        self
    }

    /// Sets the expertise level.
    #[must_use]
    pub fn with_expertise_level(mut self, level: ExpertiseLevel) -> ConfigOption {
        self.expertise_level = level;
        self
    }

    /// Sets the validation regex.
    #[must_use]
    pub fn with_validation_regex(mut self, regex: &str) -> ConfigOption {
        self.validation_regex = regex.to_string();
        self
    }

    /// Sets the possible values.
    #[must_use]
    pub fn with_possible_values(mut self, values: Vec<PossibleValue>) -> ConfigOption {
        self.possible_values = values;
        self
    }

    /// Sets an annotation.
    #[must_use]
    pub fn with_annotation(mut self, key: &str, value: Value) -> ConfigOption {
        self.annotations
            .get_or_insert_with(Map::new)
            .insert(key.to_string(), value);
        self
    }

    /// Sets the additional validation function.
    #[must_use]
    pub fn with_validation_func(
        mut self,
        func: impl Fn(&Value) -> Result<(), String> + Send + Sync + 'static,
    ) -> ConfigOption {
        self.validation_func = Some(ValidationFunc::new(func));
        self
    }

    /// Adds a value migration, applied before validation.
    #[must_use]
    pub fn with_migration(
        mut self,
        func: impl Fn(&ConfigOption, &Value) -> Value + Send + Sync + 'static,
    ) -> ConfigOption {
        self.migrations.push(MigrationFunc::new(func));
        self
    }

    /// Marks the option as requiring a restart.
    #[must_use]
    pub fn with_requires_restart(mut self, requires_restart: bool) -> ConfigOption {
        self.requires_restart = requires_restart;
        self
    }
}

/// A validated configuration value.
#[derive(Debug, Clone, PartialEq)]
pub enum CachedValue {
    String(String),
    StringArray(Vec<String>),
    Int(i64),
    Bool(bool),
}

impl CachedValue {
    pub fn to_json(&self) -> Value {
        match self {
            CachedValue::String(value) => Value::String(value.clone()),
            CachedValue::StringArray(values) => {
                Value::Array(values.iter().cloned().map(Value::String).collect())
            }
            CachedValue::Int(value) => Value::from(*value),
            CachedValue::Bool(value) => Value::Bool(*value),
        }
    }
}

/// A configuration value validation error.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[error("validation of {key} failed: {message}")]
pub struct ValidationError {
    pub key: String,
    pub message: String,
}

impl ValidationError {
    fn new(key: &str, message: impl Into<String>) -> ValidationError {
        ValidationError {
            key: key.to_string(),
            message: message.into(),
        }
    }
}

/// Returns the Go type name of a JSON value, as used in error messages.
fn go_type_name(value: &Value) -> &'static str {
    match value {
        Value::Null => "<nil>",
        Value::Bool(_) => "bool",
        Value::Number(number) if number.is_i64() || number.is_u64() => "int",
        Value::Number(_) => "float64",
        Value::String(_) => "string",
        Value::Array(_) => "[]interface {}",
        Value::Object(_) => "map[string]interface {}",
    }
}

/// Checks whether the value is one of the allowed possible values.
fn is_allowed_possible_value(option: &ConfigOption, value: &Value) -> Result<(), ValidationError> {
    if option.possible_values.is_empty() {
        return Ok(());
    }

    for possible in &option.possible_values {
        // Go converts numeric types before comparing; JSON numbers compare
        // numerically here, everything else structurally.
        if possible.value == *value {
            return Ok(());
        }
        if let (Some(a), Some(b)) = (possible.value.as_f64(), value.as_f64()) {
            if a == b {
                return Ok(());
            }
        }
    }

    Err(ValidationError::new(&option.key, "value is not allowed"))
}

/// Validates a value against an option, like Go `validateValue`.
pub fn validate_value(
    option: &ConfigOption,
    regex: Option<&Regex>,
    value: &Value,
) -> Result<CachedValue, ValidationError> {
    if option.opt_type != OPT_TYPE_STRING_ARRAY {
        is_allowed_possible_value(option, value)?;
    }

    let validated = match value {
        Value::String(text) => {
            if option.opt_type != OPT_TYPE_STRING {
                return Err(expected_type_error(option, value));
            }
            if let Some(regex) = regex {
                if !regex.is_match(text) {
                    return Err(ValidationError::new(
                        &option.key,
                        "did not match validation regex",
                    ));
                }
            }
            CachedValue::String(text.clone())
        }
        Value::Array(entries) => {
            let mut converted = Vec::with_capacity(entries.len());
            for (position, entry) in entries.iter().enumerate() {
                let Value::String(text) = entry else {
                    return Err(ValidationError::new(
                        &option.key,
                        format!("entry #{} is not a string", position + 1),
                    ));
                };
                converted.push(text.clone());
            }
            validate_string_array(option, regex, converted)?
        }
        Value::Number(number) => {
            if option.opt_type != OPT_TYPE_INT {
                return Err(expected_type_error(option, value));
            }
            let as_int = if let Some(int) = number.as_i64() {
                int
            } else if let Some(float) = number.as_f64() {
                if float.fract() != 0.0 {
                    return Err(ValidationError::new(
                        &option.key,
                        "failed to convert float64 to int64",
                    ));
                }
                float as i64
            } else {
                return Err(ValidationError::new(
                    &option.key,
                    "failed to convert float64 to int64",
                ));
            };
            if let Some(regex) = regex {
                if !regex.is_match(&as_int.to_string()) {
                    return Err(ValidationError::new(
                        &option.key,
                        "did not match validation regex",
                    ));
                }
            }
            CachedValue::Int(as_int)
        }
        Value::Bool(flag) => {
            if option.opt_type != OPT_TYPE_BOOL {
                return Err(expected_type_error(option, value));
            }
            CachedValue::Bool(*flag)
        }
        Value::Null | Value::Object(_) => {
            return Err(ValidationError::new(
                &option.key,
                format!("invalid option value type: {}", go_type_name(value)),
            ))
        }
    };

    if let Some(func) = &option.validation_func {
        func.call(&validated.to_json())
            .map_err(|message| ValidationError::new(&option.key, message))?;
    }

    Ok(validated)
}

fn validate_string_array(
    option: &ConfigOption,
    regex: Option<&Regex>,
    values: Vec<String>,
) -> Result<CachedValue, ValidationError> {
    if option.opt_type != OPT_TYPE_STRING_ARRAY {
        return Err(ValidationError::new(
            &option.key,
            format!(
                "expected type {}, got type []string",
                type_name(option.opt_type)
            ),
        ));
    }
    for (position, entry) in values.iter().enumerate() {
        if let Some(regex) = regex {
            if !regex.is_match(entry) {
                return Err(ValidationError::new(
                    &option.key,
                    format!("entry #{} did not match validation regex", position + 1),
                ));
            }
        }
        if is_allowed_possible_value(option, &Value::String(entry.clone())).is_err() {
            return Err(ValidationError::new(
                &option.key,
                format!("entry #{} is not allowed", position + 1),
            ));
        }
    }
    Ok(CachedValue::StringArray(values))
}

fn expected_type_error(option: &ConfigOption, value: &Value) -> ValidationError {
    ValidationError::new(
        &option.key,
        format!(
            "expected type {}, got type {}",
            type_name(option.opt_type),
            go_type_name(value)
        ),
    )
}

/// Runtime values of an option.
#[derive(Debug)]
pub(crate) struct OptionValues {
    /// Value set by the user or loaded from the config file.
    pub active: Option<CachedValue>,
    /// Default value set internally.
    pub active_default: Option<CachedValue>,
    /// Validated default value from registration.
    pub fallback: CachedValue,
    /// Current annotations, which may change at runtime.
    pub annotations: BTreeMap<String, Value>,
}

/// A registered option with its runtime state.
#[derive(Debug)]
pub struct OptionState {
    option: ConfigOption,
    regex: Option<Regex>,
    values: std::sync::RwLock<OptionValues>,
}

impl OptionState {
    pub(crate) fn new(option: ConfigOption, regex: Option<Regex>) -> OptionState {
        let fallback = match validate_value(&option, regex.as_ref(), &option.default_value) {
            Ok(value) => value,
            Err(_) => CachedValue::String(String::new()),
        };
        let annotations = option
            .annotations
            .as_ref()
            .map(|map| {
                map.iter()
                    .map(|(key, value)| (key.clone(), value.clone()))
                    .collect()
            })
            .unwrap_or_default();
        OptionState {
            option,
            regex,
            values: std::sync::RwLock::new(OptionValues {
                active: None,
                active_default: None,
                fallback,
                annotations,
            }),
        }
    }

    pub fn key(&self) -> &str {
        &self.option.key
    }

    /// Returns the immutable definition of the option.
    pub fn definition(&self) -> &ConfigOption {
        &self.option
    }

    pub fn is_set_by_user(&self) -> bool {
        self.values.read().expect("lock poisoned").active.is_some()
    }

    pub fn user_value(&self) -> Option<CachedValue> {
        self.values.read().expect("lock poisoned").active.clone()
    }

    pub fn active_default_value(&self) -> Option<CachedValue> {
        self.values
            .read()
            .expect("lock poisoned")
            .active_default
            .clone()
    }

    pub fn fallback_value(&self) -> CachedValue {
        self.values.read().expect("lock poisoned").fallback.clone()
    }

    pub fn annotation(&self, key: &str) -> Option<Value> {
        self.values
            .read()
            .expect("lock poisoned")
            .annotations
            .get(key)
            .cloned()
    }

    /// Adds an annotation if the key is not set yet.
    pub fn add_annotation(&self, key: &str, value: Value) {
        self.values
            .write()
            .expect("lock poisoned")
            .annotations
            .entry(key.to_string())
            .or_insert(value);
    }

    /// Sets an annotation, overwriting an existing value.
    pub fn set_annotation(&self, key: &str, value: Value) {
        self.values
            .write()
            .expect("lock poisoned")
            .annotations
            .insert(key.to_string(), value);
    }

    /// Returns the current annotations.
    pub fn annotations(&self) -> Map<String, Value> {
        let values = self.values.read().expect("lock poisoned");
        values
            .annotations
            .iter()
            .map(|(key, value)| (key.clone(), value.clone()))
            .collect()
    }

    /// Serializes the option like Go `Option.Export`, including active values.
    pub fn export(&self) -> Value {
        let values = self.values.read().expect("lock poisoned");
        export_json(&self.option, &values)
    }
}

fn export_json(option: &ConfigOption, values: &OptionValues) -> Value {
    let mut json = serde_json::to_value(option).expect("options are serializable");
    let Some(object) = json.as_object_mut() else {
        return json;
    };

    object.insert(
        "Annotations".to_string(),
        if values.annotations.is_empty() {
            Value::Null
        } else {
            Value::Object(
                values
                    .annotations
                    .iter()
                    .map(|(key, value)| (key.clone(), value.clone()))
                    .collect(),
            )
        },
    );

    // Go replaces DefaultValue in place and appends Value.
    if let Some(default) = &values.active_default {
        object.insert("DefaultValue".to_string(), default.to_json());
    }
    if let Some(active) = &values.active {
        object.insert("Value".to_string(), active.to_json());
    }

    json
}

/// Which value layer is written.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum LayerTarget {
    User,
    Default,
}

impl OptionState {
    /// Validates a value against this option.
    ///
    /// The option's migrations run first, like Go `migrateValue`.
    pub fn validate(&self, value: &Value) -> Result<CachedValue, ValidationError> {
        let mut migrated = std::borrow::Cow::Borrowed(value);
        for migration in &self.option.migrations {
            migrated = std::borrow::Cow::Owned(migration.call(&self.option, &migrated));
        }
        validate_value(&self.option, self.regex.as_ref(), &migrated)
    }

    /// Sets or clears a value layer. On a validation error the layer is
    /// left unchanged.
    pub(crate) fn set_layer_value(
        &self,
        layer: LayerTarget,
        value: Option<&Value>,
    ) -> Result<(), ValidationError> {
        // Validate before taking the write lock so a failure changes nothing.
        let cached = match value {
            None => None,
            Some(value) => Some(self.validate(value)?),
        };

        let mut values = self.values.write().expect("lock poisoned");
        match layer {
            LayerTarget::User => values.active = cached,
            LayerTarget::Default => values.active_default = cached,
        }
        Ok(())
    }

    /// Clears a value layer without validation.
    pub(crate) fn clear_layer(&self, layer: LayerTarget) {
        let mut values = self.values.write().expect("lock poisoned");
        match layer {
            LayerTarget::User => values.active = None,
            LayerTarget::Default => values.active_default = None,
        }
    }
}
