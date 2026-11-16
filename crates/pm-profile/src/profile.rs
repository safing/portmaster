//! The profile model.

use std::sync::atomic::{AtomicBool, Ordering};

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

/// The outdated marker of a profile (Go `outdated` flag).
///
/// Cloning yields an unset flag, so a copy of a profile is never flagged by
/// marking the original; share a profile through an `Arc` to share the flag.
#[derive(Debug, Default)]
pub struct OutdatedFlag(AtomicBool);

impl Clone for OutdatedFlag {
    fn clone(&self) -> Self {
        OutdatedFlag::default()
    }
}

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
    /// Set when a newer version of the profile exists. Use
    /// [`Profile::mark_outdated`]; a clone starts with the flag unset.
    #[serde(skip)]
    pub outdated: OutdatedFlag,
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
            outdated: OutdatedFlag::default(),
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

    /// Marks the profile as outdated, so layered profiles re-layer it.
    pub fn mark_outdated(&self) {
        self.outdated.0.store(true, Ordering::SeqCst);
    }

    /// Returns whether the profile was marked as outdated.
    pub fn is_outdated(&self) -> bool {
        self.outdated.0.load(Ordering::SeqCst)
    }

    /// Clears the outdated flag.
    pub(crate) fn clear_outdated(&self) {
        self.outdated.0.store(false, Ordering::SeqCst);
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
