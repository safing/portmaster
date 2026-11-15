//! The profile model.

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

use crate::fingerprint::{derive_profile_id, Fingerprint};
use crate::icon::Icon;
use crate::{make_profile_key, make_scoped_id};
use pm_dsd::gotime::{format_rfc3339, parse_rfc3339};

/// Default action is not set.
pub const DEFAULT_ACTION_NOT_SET: u8 = 0;
/// Default action: block.
pub const DEFAULT_ACTION_BLOCK: u8 = 1;
/// Default action: ask.
pub const DEFAULT_ACTION_ASK: u8 = 2;
/// Default action: permit.
pub const DEFAULT_ACTION_PERMIT: u8 = 3;

/// Configuration value of the permit default action.
pub const DEFAULT_ACTION_PERMIT_VALUE: &str = "permit";
/// Configuration value of the block default action.
pub const DEFAULT_ACTION_BLOCK_VALUE: &str = "block";
/// Configuration value of the ask default action.
pub const DEFAULT_ACTION_ASK_VALUE: &str = "ask";

/// Configuration key of the default action.
pub const CFG_OPTION_DEFAULT_ACTION_KEY: &str = "filter/defaultAction";
/// Configuration key of the endpoint rules.
pub const CFG_OPTION_ENDPOINTS_KEY: &str = "filter/endpoints";
/// Configuration key of the service endpoint rules.
pub const CFG_OPTION_SERVICE_ENDPOINTS_KEY: &str = "filter/serviceEndpoints";
/// Configuration key of the filter lists.
pub const CFG_OPTION_FILTER_LISTS_KEY: &str = "filter/lists";
/// Configuration key of the split-tunnel usage policy.
pub const CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY: &str = "splittun/usagePolicy";

/// A profile that predefines the security settings for applications.
///
/// Field names and order match the Go struct.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Profile {
    #[serde(rename = "ID")]
    pub id: String,
    #[serde(rename = "Source")]
    pub source: crate::ProfileSource,
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Description")]
    pub description: String,
    #[serde(rename = "Warning")]
    pub warning: String,
    #[serde(
        rename = "WarningLastUpdated",
        serialize_with = "serialize_time",
        deserialize_with = "deserialize_time",
        default
    )]
    pub warning_last_updated: i64,
    #[serde(rename = "Homepage")]
    pub homepage: String,
    #[serde(rename = "Icon")]
    pub icon: String,
    #[serde(rename = "IconType")]
    pub icon_type: crate::IconType,
    #[serde(rename = "Icons")]
    pub icons: Vec<Icon>,
    #[serde(rename = "LinkedPath")]
    pub linked_path: String,
    #[serde(rename = "PresentationPath")]
    pub presentation_path: String,
    #[serde(rename = "UsePresentationPath")]
    pub use_presentation_path: bool,
    #[serde(rename = "Fingerprints")]
    pub fingerprints: Vec<Fingerprint>,
    #[serde(rename = "Config")]
    pub config: Map<String, Value>,
    #[serde(rename = "LastEdited")]
    pub last_edited: i64,
    #[serde(rename = "Created")]
    pub created: i64,
    #[serde(rename = "Internal")]
    pub internal: bool,
    /// Whether the profile was saved by the core itself; not serialized.
    #[serde(skip)]
    pub saved_internally: bool,
}

fn serialize_time<S: serde::Serializer>(seconds: &i64, serializer: S) -> Result<S::Ok, S::Error> {
    serializer.serialize_str(&format_rfc3339(*seconds))
}

fn deserialize_time<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<i64, D::Error> {
    let text = String::deserialize(deserializer)?;
    Ok(parse_rfc3339(&text).unwrap_or(0))
}

impl Default for Profile {
    fn default() -> Self {
        Profile {
            id: String::new(),
            source: String::new(),
            name: String::new(),
            description: String::new(),
            warning: String::new(),
            warning_last_updated: 0,
            homepage: String::new(),
            icon: String::new(),
            icon_type: String::new(),
            icons: Vec::new(),
            linked_path: String::new(),
            presentation_path: String::new(),
            use_presentation_path: false,
            fingerprints: Vec::new(),
            config: Map::new(),
            last_edited: 0,
            created: 0,
            internal: false,
            saved_internally: false,
        }
    }
}

impl Profile {
    /// Creates a new profile, like Go `profile.New`.
    ///
    /// Expands a flat config, derives the ID from the fingerprints when it is
    /// empty and sets the creation timestamp.
    pub fn new(mut profile: Profile) -> Profile {
        profile.created = crate::now();
        profile.saved_internally = true;

        // Expand a flat config (`key/value` paths) into a nested object.
        profile.config = pm_config::expand(&profile.config);

        // Derive the profile ID, if it is not set.
        if profile.id.is_empty() {
            if profile.fingerprints.is_empty() {
                profile.id = random_uuid();
            } else {
                profile.id = derive_profile_id(&mut profile.fingerprints);
            }
        }

        // Migrate the deprecated LinkedPath to the PresentationPath.
        if profile.presentation_path.is_empty() && !profile.linked_path.is_empty() {
            profile.presentation_path = profile.linked_path.clone();
        }

        profile
    }

    /// Returns the scoped ID (`source/id`).
    pub fn scoped_id(&self) -> String {
        make_scoped_id(&self.source, &self.id)
    }

    /// Returns the database key (`core:profiles/source/id`).
    pub fn key(&self) -> String {
        make_profile_key(&self.source, &self.id)
    }

    /// Parses the profile configuration.
    pub fn parse_config(&self) -> (ProfileConfig, Option<String>) {
        ProfileConfig::from_config(&self.config)
    }

    /// Returns the layered profile of this profile.
    ///
    /// Config layers are not merged yet; the layered profile carries the
    /// settings of this profile.
    pub fn layered_profile(&self) -> LayeredProfile {
        let (config, error) = self.parse_config();
        LayeredProfile {
            local_profile_id: self.scoped_id(),
            config,
            error,
            updated: crate::now(),
        }
    }
}

/// Parsed settings of a profile.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ProfileConfig {
    pub default_action: u8,
    /// Raw endpoint rule strings; parsing arrives with a `pm-endpoints` crate.
    pub endpoints: Vec<String>,
    pub service_endpoints: Vec<String>,
    pub filter_lists: Vec<String>,
    pub split_tun_usage_policy: Vec<String>,
}

impl ProfileConfig {
    /// Parses the settings from a (nested) profile config map.
    pub fn from_config(config: &Map<String, Value>) -> (ProfileConfig, Option<String>) {
        let mut parsed = ProfileConfig::default();
        let mut first_error = None;

        if let Some(action) = get_string(config, CFG_OPTION_DEFAULT_ACTION_KEY) {
            match action.as_str() {
                DEFAULT_ACTION_PERMIT_VALUE => parsed.default_action = DEFAULT_ACTION_PERMIT,
                DEFAULT_ACTION_ASK_VALUE => parsed.default_action = DEFAULT_ACTION_ASK,
                DEFAULT_ACTION_BLOCK_VALUE => parsed.default_action = DEFAULT_ACTION_BLOCK,
                _ => first_error = Some(format!("default action \"{action}\" invalid")),
            }
        }

        parsed.endpoints = get_string_array(config, CFG_OPTION_ENDPOINTS_KEY);
        parsed.service_endpoints = get_string_array(config, CFG_OPTION_SERVICE_ENDPOINTS_KEY);
        parsed.filter_lists = get_string_array(config, CFG_OPTION_FILTER_LISTS_KEY);
        parsed.split_tun_usage_policy =
            get_string_array(config, CFG_OPTION_SPLIT_TUN_USAGE_POLICY_KEY);

        (parsed, first_error)
    }
}

/// A view on a profile with its effective settings.
#[derive(Debug, Clone)]
pub struct LayeredProfile {
    pub local_profile_id: String,
    pub config: ProfileConfig,
    pub error: Option<String>,
    pub updated: i64,
}

impl LayeredProfile {
    /// Returns the default action.
    pub fn default_action(&self) -> u8 {
        self.config.default_action
    }

    /// Returns the endpoint rule strings.
    pub fn endpoints(&self) -> &[String] {
        &self.config.endpoints
    }
}

/// Looks up a nested config value by a flat key (`a/b/c`).
fn lookup<'a>(config: &'a Map<String, Value>, key: &str) -> Option<&'a Value> {
    let mut parts = key.split('/');
    let mut current = config.get(parts.next()?)?;
    for part in parts {
        current = current.as_object()?.get(part)?;
    }
    Some(current)
}

/// Returns a string setting.
fn get_string(config: &Map<String, Value>, key: &str) -> Option<String> {
    lookup(config, key)?.as_str().map(str::to_string)
}

/// Returns a string array setting.
fn get_string_array(config: &Map<String, Value>, key: &str) -> Vec<String> {
    match lookup(config, key) {
        Some(Value::Array(entries)) => entries
            .iter()
            .filter_map(|entry| entry.as_str().map(str::to_string))
            .collect(),
        _ => Vec::new(),
    }
}

/// Returns a random UUID v4 string.
fn random_uuid() -> String {
    let mut bytes = [0u8; 16];
    if getrandom::fill(&mut bytes).is_err() {
        return String::new();
    }
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    let hex = hex::encode(bytes);
    format!(
        "{}-{}-{}-{}-{}",
        &hex[0..8],
        &hex[8..12],
        &hex[12..16],
        &hex[16..20],
        &hex[20..32]
    )
}

/// Returns the current time in Unix seconds.
pub fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}

/// Validates a profile like Go `EnsureProfile`.
pub fn ensure_profile(profile: &Profile) -> Result<(), String> {
    if profile.id.is_empty() {
        return Err("profile has no ID".to_string());
    }
    if profile.source.is_empty() {
        return Err(format!("profile {} does not specify a source", profile.id));
    }
    Ok(())
}
