//! Compatible implementation of the Go `base/config` package.
//!
//! Options are registered with [`Config::register`], values can be layered as
//! user values, internal defaults and registration defaults, and everything is
//! persisted as a hierarchical `config.json` that is byte-compatible with Go.
//!
//! # Compatibility notes
//!
//! * Getters return closures that re-read the value when the configuration
//!   generation changes, like Go's `ValidityFlag`-based getters.
//! * The Go `Option` type is called [`ConfigOption`] here; JSON field names and
//!   order are identical.
//! * Unknown keys from a loaded `config.json` are preserved on save (the Go
//!   `SaveConfig` drops them). This is deliberate: user data is not lost, and
//!   files written for known keys are byte-identical to Go.
//! * Validation functions ([`ValidationFunc`]) are supported and receive the
//!   validated value as JSON. Value migrations ([`MigrationFunc`]) run before
//!   validation when values are set, replaced, loaded or read by a [`Perspective`].
//! * The interface cache of the Go database layer is not used; the config
//!   storage is served from the option registry.

mod option;
mod persistence;
mod perspective;
mod storage;

use std::cell::{Cell, RefCell};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicU8, Ordering};
use std::sync::{Arc, Mutex, OnceLock, RwLock, Weak};

use regex::Regex;
use serde_json::{Map, Value};
use tokio::sync::broadcast;

pub use option::{
    migrate_security_level_to_boolean, type_name, CachedValue, ConfigOption, ExpertiseLevel,
    MigrationFunc, OptionState, OptionType, PossibleValue, ReleaseLevel, ValidationError,
    ValidationFunc, CATEGORY_ANNOTATION, DISPLAY_HINT_ANNOTATION, DISPLAY_HINT_FILE_PICKER,
    DISPLAY_HINT_ONE_OF, DISPLAY_HINT_ORDERED, DISPLAY_ORDER_ANNOTATION, EXPERTISE_LEVEL_DEVELOPER,
    EXPERTISE_LEVEL_EXPERT, EXPERTISE_LEVEL_USER, OPT_TYPE_ANY, OPT_TYPE_BOOL, OPT_TYPE_INT,
    OPT_TYPE_STRING, OPT_TYPE_STRING_ARRAY, QUICK_SETTINGS_ANNOTATION, RELEASE_LEVEL_BETA,
    RELEASE_LEVEL_EXPERIMENTAL, RELEASE_LEVEL_STABLE, REQUIRES_ANNOTATION,
    REQUIRES_FEATURE_ID_ANNOTATION, REQUIRES_UI_RELOAD_ANNOTATION, RESTART_PENDING_ANNOTATION,
    SETTABLE_PER_APP_ANNOTATION, STACKABLE_ANNOTATION, SUBSYSTEM_ANNOTATION, UNIT_ANNOTATION,
};
pub use persistence::{
    expand, flatten, json_to_map, map_to_json, put_value_into_hierarchical_config,
};
pub use perspective::Perspective;
pub use storage::ConfigStorage;

use pm_database::{Controller, Databases, Record};

/// Key of the expertise level option.
pub const EXPERTISE_LEVEL_KEY: &str = "core/expertiseLevel";
/// Key of the release level option.
pub const RELEASE_LEVEL_KEY: &str = "core/releaseLevel";

/// Errors of the configuration system.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("option {0:?} does not exist")]
    UnknownOption(String),
    #[error("{0}")]
    InvalidOption(String),
    #[error("cannot register option '{0}': config already initialized")]
    AlreadyInitialized(String),
    #[error("config file is not a JSON object: {0}")]
    InvalidJsonConfig(String),
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("database: {0}")]
    Database(#[from] pm_database::Error),
    #[error(transparent)]
    Validation(#[from] ValidationError),
}

/// Result type of this crate.
pub type Result<T> = std::result::Result<T, Error>;

use option::LayerTarget as Layer;

/// Outcome of loading a configuration file.
#[derive(Debug, Clone, Default)]
pub struct LoadOutcome {
    pub validation_errors: Vec<ValidationError>,
    pub requires_restart: bool,
    pub contains_unknown: bool,
}

/// Result of validating a set of config values.
#[derive(Debug, Clone, Default)]
pub struct ValidationOutcome {
    pub validation_errors: Vec<ValidationError>,
    pub requires_restart: bool,
    pub contains_unknown: bool,
}

/// The configuration system: option registry, layers and persistence.
pub struct Config {
    options: RwLock<BTreeMap<String, Arc<OptionState>>>,
    initialized: AtomicBool,
    generation: AtomicU64,
    expertise_level: AtomicU8,
    release_level: AtomicU8,
    config_file: RwLock<Option<PathBuf>>,
    unknown: RwLock<BTreeMap<String, Value>>,
    loaded_errors: Mutex<Vec<ValidationError>>,
    change_tx: broadcast::Sender<()>,
    db_controller: RwLock<Option<Weak<Controller>>>,
}

impl Config {
    /// Creates a config system and registers the built-in level options.
    pub fn new() -> Arc<Config> {
        let config = Arc::new(Config {
            options: RwLock::new(BTreeMap::new()),
            initialized: AtomicBool::new(false),
            generation: AtomicU64::new(0),
            expertise_level: AtomicU8::new(EXPERTISE_LEVEL_USER),
            release_level: AtomicU8::new(RELEASE_LEVEL_STABLE),
            config_file: RwLock::new(None),
            unknown: RwLock::new(BTreeMap::new()),
            loaded_errors: Mutex::new(Vec::new()),
            change_tx: broadcast::channel(64).0,
            db_controller: RwLock::new(None),
        });

        config
            .register(expertise_level_option())
            .expect("built-in expertise level option is valid");
        config
            .register(release_level_option())
            .expect("built-in release level option is valid");

        config
    }

    // --- registry ---

    /// Registers a configuration option, like Go `config.Register`.
    pub fn register(&self, mut option: ConfigOption) -> Result<()> {
        if option.name.is_empty() {
            return Err(Error::InvalidOption(
                "failed to register option: please set option.Name".to_string(),
            ));
        }
        if option.key.is_empty() {
            return Err(Error::InvalidOption(
                "failed to register option: please set option.Key".to_string(),
            ));
        }
        if option.description.is_empty() {
            return Err(Error::InvalidOption(
                "failed to register option: please set option.Description".to_string(),
            ));
        }
        if option.opt_type == OPT_TYPE_ANY {
            return Err(Error::InvalidOption(
                "failed to register option: please set option.OptType".to_string(),
            ));
        }
        if self.initialized.load(Ordering::SeqCst) {
            return Err(Error::AlreadyInitialized(option.key));
        }

        if option.validation_regex.is_empty() && !option.possible_values.is_empty() {
            let values: Vec<String> = option
                .possible_values
                .iter()
                .map(|possible| go_value_string(&possible.value))
                .collect();
            option.validation_regex = format!("^({})$", values.join("|"));
        }

        let regex = if option.validation_regex.is_empty() {
            None
        } else {
            Some(Regex::new(&option.validation_regex).map_err(|err| {
                Error::InvalidOption(format!(
                    "config: could not compile option.ValidationRegex: {err}"
                ))
            })?)
        };

        if let Err(err) = option::validate_value(&option, regex.as_ref(), &option.default_value) {
            return Err(Error::InvalidOption(format!(
                "config: invalid default value: {err}"
            )));
        }

        self.options.write().expect("lock poisoned").insert(
            option.key.clone(),
            Arc::new(OptionState::new(option, regex)),
        );
        self.refresh_levels();
        Ok(())
    }

    /// Returns a registered option.
    pub fn option(&self, key: &str) -> Result<Arc<OptionState>> {
        self.options
            .read()
            .expect("lock poisoned")
            .get(key)
            .cloned()
            .ok_or_else(|| Error::UnknownOption(key.to_string()))
    }

    pub fn is_registered(&self, key: &str) -> bool {
        self.options
            .read()
            .expect("lock poisoned")
            .contains_key(key)
    }

    /// Returns all registered option definitions, sorted by key, with their
    /// current annotations.
    pub fn export_options(&self) -> Vec<ConfigOption> {
        let options = self.options.read().expect("lock poisoned");
        options
            .values()
            .map(|state| {
                let mut definition = state.definition().clone();
                let annotations = state.annotations();
                definition.annotations = if annotations.is_empty() {
                    None
                } else {
                    Some(annotations)
                };
                definition
            })
            .collect()
    }

    /// Marks the configuration as loaded; options can no longer be registered.
    pub fn mark_initialized(&self) {
        self.initialized.store(true, Ordering::SeqCst);
    }

    pub fn is_initialized(&self) -> bool {
        self.initialized.load(Ordering::SeqCst)
    }

    // --- levels ---

    pub fn expertise_level(&self) -> ExpertiseLevel {
        self.expertise_level.load(Ordering::SeqCst)
    }

    pub fn release_level(&self) -> ReleaseLevel {
        self.release_level.load(Ordering::SeqCst)
    }

    fn refresh_levels(&self) {
        let (expertise, release) = {
            let options = self.options.read().expect("lock poisoned");
            (
                options
                    .get(EXPERTISE_LEVEL_KEY)
                    .and_then(|state| match pick_value(state) {
                        Some(CachedValue::String(value)) => Some(match value.as_str() {
                            "expert" => EXPERTISE_LEVEL_EXPERT,
                            "developer" => EXPERTISE_LEVEL_DEVELOPER,
                            _ => EXPERTISE_LEVEL_USER,
                        }),
                        _ => None,
                    })
                    .unwrap_or(EXPERTISE_LEVEL_USER),
                options
                    .get(RELEASE_LEVEL_KEY)
                    .and_then(|state| match pick_value(state) {
                        Some(CachedValue::String(value)) => Some(match value.as_str() {
                            "beta" => RELEASE_LEVEL_BETA,
                            "experimental" => RELEASE_LEVEL_EXPERIMENTAL,
                            _ => RELEASE_LEVEL_STABLE,
                        }),
                        _ => None,
                    })
                    .unwrap_or(RELEASE_LEVEL_STABLE),
            )
        };
        self.expertise_level.store(expertise, Ordering::SeqCst);
        self.release_level.store(release, Ordering::SeqCst);
    }

    // --- getters ---

    fn resolve(&self, key: &str, expected_type: OptionType) -> Option<CachedValue> {
        let state = self.option(key).ok()?;
        if state.definition().opt_type != expected_type {
            return None;
        }
        if state.definition().release_level <= self.release_level() {
            if let Some(value) = state.user_value() {
                return Some(value);
            }
        }
        if let Some(value) = state.active_default_value() {
            return Some(value);
        }
        Some(state.fallback_value())
    }

    /// Returns the current configuration generation.
    pub fn generation(&self) -> u64 {
        self.generation.load(Ordering::SeqCst)
    }

    /// Returns a fast getter for a string option.
    pub fn get_as_string(self: &Arc<Self>, key: &str, fallback: &str) -> impl Fn() -> String {
        let config = Arc::clone(self);
        let key = key.to_string();
        let fallback = fallback.to_string();
        let generation = Cell::new(u64::MAX);
        let value = RefCell::new(fallback.clone());
        move || {
            let current = config.generation();
            if generation.get() != current {
                generation.set(current);
                let resolved = match config.resolve(&key, OPT_TYPE_STRING) {
                    Some(CachedValue::String(value)) => value,
                    _ => fallback.clone(),
                };
                *value.borrow_mut() = resolved;
            }
            value.borrow().clone()
        }
    }

    /// Returns a fast getter for a string array option.
    pub fn get_as_string_array(
        self: &Arc<Self>,
        key: &str,
        fallback: &[&str],
    ) -> impl Fn() -> Vec<String> {
        let config = Arc::clone(self);
        let key = key.to_string();
        let fallback: Vec<String> = fallback.iter().map(|item| (*item).to_string()).collect();
        let generation = Cell::new(u64::MAX);
        let value = RefCell::new(fallback.clone());
        move || {
            let current = config.generation();
            if generation.get() != current {
                generation.set(current);
                let resolved = match config.resolve(&key, OPT_TYPE_STRING_ARRAY) {
                    Some(CachedValue::StringArray(value)) => value,
                    _ => fallback.clone(),
                };
                *value.borrow_mut() = resolved;
            }
            value.borrow().clone()
        }
    }

    /// Returns a fast getter for an int option.
    pub fn get_as_int(self: &Arc<Self>, key: &str, fallback: i64) -> impl Fn() -> i64 {
        let config = Arc::clone(self);
        let key = key.to_string();
        let generation = Cell::new(u64::MAX);
        let value = Cell::new(fallback);
        move || {
            let current = config.generation();
            if generation.get() != current {
                generation.set(current);
                let resolved = match config.resolve(&key, OPT_TYPE_INT) {
                    Some(CachedValue::Int(value)) => value,
                    _ => fallback,
                };
                value.set(resolved);
            }
            value.get()
        }
    }

    /// Returns a fast getter for a bool option.
    pub fn get_as_bool(self: &Arc<Self>, key: &str, fallback: bool) -> impl Fn() -> bool {
        let config = Arc::clone(self);
        let key = key.to_string();
        let generation = Cell::new(u64::MAX);
        let value = Cell::new(fallback);
        move || {
            let current = config.generation();
            if generation.get() != current {
                generation.set(current);
                let resolved = match config.resolve(&key, OPT_TYPE_BOOL) {
                    Some(CachedValue::Bool(value)) => value,
                    _ => fallback,
                };
                value.set(resolved);
            }
            value.get()
        }
    }

    // --- setting values ---

    /// Sets the user value of an option and saves the config file.
    pub fn set_config_option(&self, key: &str, value: Value) -> Result<()> {
        self.set_value(key, Some(value), Layer::User, true, true)
    }

    /// Removes the user value of an option and saves the config file.
    pub fn unset_config_option(&self, key: &str) -> Result<()> {
        self.set_value(key, None, Layer::User, true, true)
    }

    /// Sets the internal default value of an option.
    pub fn set_default_config_option(&self, key: &str, value: Value) -> Result<()> {
        self.set_value(key, Some(value), Layer::Default, true, false)
    }

    /// Removes the internal default value of an option.
    pub fn unset_default_config_option(&self, key: &str) -> Result<()> {
        self.set_value(key, None, Layer::Default, true, false)
    }

    fn set_value(
        &self,
        key: &str,
        value: Option<Value>,
        layer: Layer,
        push: bool,
        save: bool,
    ) -> Result<()> {
        let option = self.option(key)?;
        let result = option.set_layer_value(layer, value.as_ref());

        // Go marks restart-pending even when validation failed.
        if option.definition().requires_restart {
            option.set_annotation(RESTART_PENDING_ANNOTATION, Value::Bool(true));
        }

        self.refresh_levels();
        if push {
            self.push_update(&option);
        }
        result?;

        self.signal_changes();
        if save {
            self.save_config()?;
        }
        Ok(())
    }

    /// Validates config values without applying them.
    pub fn validate_config(&self, values: &Map<String, Value>) -> ValidationOutcome {
        let options = self.options_snapshot();
        let mut outcome = ValidationOutcome::default();
        let mut checked = 0;
        for option in &options {
            let Some(value) = values.get(option.key()) else {
                continue;
            };
            checked += 1;
            if let Err(err) = option.validate(value) {
                outcome.validation_errors.push(err);
            }
            if option.definition().requires_restart {
                outcome.requires_restart = true;
            }
        }
        outcome.contains_unknown = checked < values.len();
        outcome
    }

    /// Replaces the user values with the given values, like Go `ReplaceConfig`.
    pub fn replace_config(&self, values: &Map<String, Value>) -> ValidationOutcome {
        self.replace_layer(values, Layer::User)
    }

    /// Replaces the internal default values, like Go `ReplaceDefaultConfig`.
    pub fn replace_default_config(&self, values: &Map<String, Value>) -> ValidationOutcome {
        self.replace_layer(values, Layer::Default)
    }

    fn replace_layer(&self, values: &Map<String, Value>, layer: Layer) -> ValidationOutcome {
        let options = self.options_snapshot();
        let mut outcome = ValidationOutcome::default();

        for option in &options {
            option.clear_layer(layer);
            if let Some(value) = values.get(option.key()) {
                if let Err(err) = option.set_layer_value(layer, Some(value)) {
                    outcome.validation_errors.push(err);
                }
            }
            if option.definition().requires_restart {
                outcome.requires_restart = true;
            }
            self.push_update(option);
        }

        // Keep keys of unregistered options so that saving does not drop them.
        {
            let registered: Vec<&str> = options.iter().map(|option| option.key()).collect();
            let mut unknown = self.unknown.write().expect("lock poisoned");
            unknown.clear();
            for (key, value) in values {
                if !registered.contains(&key.as_str()) {
                    unknown.insert(key.clone(), value.clone());
                }
            }
        }
        outcome.contains_unknown = !self.unknown.read().expect("lock poisoned").is_empty();

        self.refresh_levels();
        self.signal_changes();
        outcome
    }

    /// Returns the active user values, like Go `GetActiveConfigValues`.
    pub fn active_config_values(&self) -> Map<String, Value> {
        let options = self.options.read().expect("lock poisoned");
        let release_level = self.release_level();
        let mut values = Map::new();
        for option in options.values() {
            if option.definition().release_level > release_level {
                continue;
            }
            if let Some(value) = option.user_value() {
                values.insert(option.key().to_string(), value.to_json());
            }
        }
        values
    }

    /// Returns the currently known unknown keys of the config file.
    pub fn unknown_values(&self) -> BTreeMap<String, Value> {
        self.unknown.read().expect("lock poisoned").clone()
    }

    // --- persistence ---

    /// Loads and applies a configuration file.
    pub fn load_config(&self, path: impl AsRef<Path>) -> Result<LoadOutcome> {
        let path = path.as_ref().to_path_buf();
        *self.config_file.write().expect("lock poisoned") = Some(path.clone());

        let data = std::fs::read(&path)?;
        let values = persistence::json_to_map(&data)?;
        let outcome = self.replace_config(&values);

        let mut loaded_errors = self.loaded_errors.lock().expect("lock poisoned");
        *loaded_errors = outcome.validation_errors.clone();

        Ok(LoadOutcome {
            validation_errors: outcome.validation_errors,
            requires_restart: outcome.requires_restart,
            contains_unknown: outcome.contains_unknown,
        })
    }

    /// Marks the config as initialized and loads the config file.
    pub fn start(&self, path: impl AsRef<Path>) -> Result<LoadOutcome> {
        self.mark_initialized();
        self.load_config(path)
    }

    /// Sets the config file path without loading it.
    pub fn set_config_file(&self, path: impl AsRef<Path>) {
        *self.config_file.write().expect("lock poisoned") = Some(path.as_ref().to_path_buf());
    }

    pub fn config_file(&self) -> Option<PathBuf> {
        self.config_file.read().expect("lock poisoned").clone()
    }

    /// Returns the validation errors of the last load.
    pub fn loaded_validation_errors(&self) -> Vec<ValidationError> {
        self.loaded_errors.lock().expect("lock poisoned").clone()
    }

    /// Saves the user values to the config file.
    pub fn save_config(&self) -> Result<()> {
        let Some(path) = self.config_file() else {
            return Ok(());
        };

        let mut flattened = self.active_config_values();
        {
            let options = self.options.read().expect("lock poisoned");
            for (key, value) in self.unknown.read().expect("lock poisoned").iter() {
                if !options.contains_key(key) {
                    flattened.insert(key.clone(), value.clone());
                }
            }
        }

        let data = persistence::map_to_json(&flattened)?;
        write_file(&path, &data)?;
        Ok(())
    }

    // --- export and events ---

    /// Returns the export JSON of an option, like Go `Option.Export`.
    pub fn export_option(&self, key: &str) -> Result<Value> {
        Ok(self.option(key)?.export())
    }

    /// Returns an exported option as a `config:<key>` database record.
    pub fn export_record(&self, key: &str) -> Result<Record> {
        let json = self.export_option(key)?;
        Ok(Record {
            key: pm_record::Key::new("config", key),
            meta: pm_record::Meta::default(),
            format: pm_dsd::format::JSON,
            data: pm_dsd::gojson::to_vec(&json)?,
        })
    }

    /// Subscribes to configuration change events.
    pub fn subscribe_changes(&self) -> broadcast::Receiver<()> {
        self.change_tx.subscribe()
    }

    fn signal_changes(&self) {
        self.generation.fetch_add(1, Ordering::SeqCst);
        let _ = self.change_tx.send(());
    }

    fn push_update(&self, option: &Arc<OptionState>) {
        let controller = self
            .db_controller
            .read()
            .expect("lock poisoned")
            .as_ref()
            .and_then(Weak::upgrade);
        let Some(controller) = controller else {
            return;
        };
        if let Ok(record) = self.export_record(option.key()) {
            controller.push_update(&record);
        }
    }

    pub(crate) fn options_snapshot(&self) -> Vec<Arc<OptionState>> {
        self.options
            .read()
            .expect("lock poisoned")
            .values()
            .cloned()
            .collect()
    }

    // --- database integration ---

    /// Registers and injects the `config` database, like Go `registerAsDatabase`.
    pub fn register_database(self: &Arc<Self>, dbs: &Arc<Databases>) -> Result<Arc<Controller>> {
        dbs.register_injected("config", "Configuration Manager")?;
        let controller = dbs.inject("config", Box::new(ConfigStorage::new(Arc::clone(self))))?;
        *self.db_controller.write().expect("lock poisoned") = Some(Arc::downgrade(&controller));
        Ok(controller)
    }
}

impl Config {
    /// Creates a perspective on the given nested configuration.
    pub fn perspective(self: &Arc<Self>, nested: &Value) -> (Perspective, Option<Error>) {
        Perspective::new(self, nested)
    }
}

/// Returns the value Go's getters would use: the fallback, overridden by the
/// user value and then by the internal default value, like Go's level update.
fn pick_value(option: &OptionState) -> Option<CachedValue> {
    let mut value = Some(option.fallback_value());
    if let Some(active) = option.user_value() {
        value = Some(active);
    }
    if let Some(default) = option.active_default_value() {
        value = Some(default);
    }
    value
}

/// Formats a JSON value like Go's `%v`.
fn go_value_string(value: &Value) -> String {
    match value {
        Value::String(text) => text.clone(),
        Value::Bool(flag) => flag.to_string(),
        Value::Number(number) => number.to_string(),
        Value::Null => "<nil>".to_string(),
        Value::Array(entries) => {
            let items: Vec<String> = entries.iter().map(go_value_string).collect();
            format!("[{}]", items.join(" "))
        }
        Value::Object(_) => "map[]".to_string(),
    }
}

#[cfg(unix)]
fn write_file(path: &Path, data: &[u8]) -> std::io::Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;

    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o600)
        .open(path)?;
    file.write_all(data)
}

#[cfg(not(unix))]
fn write_file(path: &Path, data: &[u8]) -> std::io::Result<()> {
    std::fs::write(path, data)
}

fn expertise_level_option() -> ConfigOption {
    ConfigOption {
        name: "UI Mode".to_string(),
        key: EXPERTISE_LEVEL_KEY.to_string(),
        description: "Control the default amount of settings and information shown. Hidden settings are still in effect. Can be changed temporarily in the top right corner.".to_string(),
        opt_type: OPT_TYPE_STRING,
        expertise_level: EXPERTISE_LEVEL_USER,
        release_level: RELEASE_LEVEL_STABLE,
        default_value: Value::String("user".to_string()),
        annotations: Some(serde_json::json!({
            DISPLAY_ORDER_ANNOTATION: -16,
            DISPLAY_HINT_ANNOTATION: DISPLAY_HINT_ONE_OF,
            CATEGORY_ANNOTATION: "User Interface",
        }).as_object().cloned().unwrap()),
        possible_values: vec![
            PossibleValue {
                name: "Simple Interface".to_string(),
                value: Value::String("user".to_string()),
                description: "Hide complex settings and information.".to_string(),
            },
            PossibleValue {
                name: "Advanced Interface".to_string(),
                value: Value::String("expert".to_string()),
                description: "Show technical details.".to_string(),
            },
            PossibleValue {
                name: "Developer Interface".to_string(),
                value: Value::String("developer".to_string()),
                description: "Developer mode. Please be careful!".to_string(),
            },
        ],
        ..ConfigOption::default()
    }
}

fn release_level_option() -> ConfigOption {
    ConfigOption {
        name: "Feature Stability".to_string(),
        key: RELEASE_LEVEL_KEY.to_string(),
        description: "May break things. Decide if you want to experiment with unstable features. \"Beta\" has been tested roughly by the Safing team while \"Experimental\" is really raw. When \"Beta\" or \"Experimental\" are disabled, their settings use the default again.".to_string(),
        opt_type: OPT_TYPE_STRING,
        expertise_level: EXPERTISE_LEVEL_DEVELOPER,
        release_level: RELEASE_LEVEL_STABLE,
        default_value: Value::String("stable".to_string()),
        annotations: Some(serde_json::json!({
            DISPLAY_ORDER_ANNOTATION: -8,
            DISPLAY_HINT_ANNOTATION: DISPLAY_HINT_ONE_OF,
            CATEGORY_ANNOTATION: "Updates",
        }).as_object().cloned().unwrap()),
        possible_values: vec![
            PossibleValue {
                name: "Stable".to_string(),
                value: Value::String("stable".to_string()),
                description: "Only show stable features.".to_string(),
            },
            PossibleValue {
                name: "Beta".to_string(),
                value: Value::String("beta".to_string()),
                description: "Show stable and beta features.".to_string(),
            },
            PossibleValue {
                name: "Experimental".to_string(),
                value: Value::String("experimental".to_string()),
                description: "Show all features".to_string(),
            },
        ],
        ..ConfigOption::default()
    }
}

/// Returns a process-wide default config, matching Go's package-level registry.
pub fn global() -> Arc<Config> {
    static GLOBAL: OnceLock<Arc<Config>> = OnceLock::new();
    Arc::clone(GLOBAL.get_or_init(Config::new))
}
