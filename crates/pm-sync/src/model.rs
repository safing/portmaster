//! Wire format of the share-able exports.
//!
//! JSON names, `omitempty` behaviour and field order match the Go
//! `service/sync` structs. The JSON is later canonicalised by the jess
//! checksum step; the order here is what Go's `json.Marshal` produces.
//!
//! Missing fields default to their Go zero value on import; this is what
//! `encoding/json.Unmarshal` does for absent struct members.

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

/// Type of an export (`Type` in Go).
pub const TYPE_PROFILE: &str = "profile";
/// Type of a settings export.
pub const TYPE_SETTINGS: &str = "settings";
/// Type of a single-setting export.
pub const TYPE_SINGLE_SETTING: &str = "single-setting";

/// Target for global settings (`ExportTargetGlobal` in Go).
pub const EXPORT_TARGET_GLOBAL: &str = "global";

fn is_false(value: &bool) -> bool {
    !*value
}

/// Request for an export (`ExportRequest` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ExportRequest {
    #[serde(rename = "from")]
    pub from: String,
    #[serde(rename = "keys")]
    pub keys: Vec<String>,
}

/// Common part of an import request (`ImportRequest` in Go, JSON inlined).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ImportRequest {
    #[serde(rename = "target")]
    pub target: String,
    #[serde(rename = "validateOnly")]
    pub validate_only: bool,
    #[serde(rename = "rawExport")]
    pub raw_export: String,
    #[serde(rename = "rawMime")]
    pub raw_mime: String,
}

/// Common part of an import result (`ImportResult` in Go, JSON inlined).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ImportResult {
    #[serde(rename = "restartRequired")]
    pub restart_required: bool,
    #[serde(rename = "replacesExisting")]
    pub replaces_existing: bool,
    #[serde(rename = "containsUnknown")]
    pub contains_unknown: bool,
}

/// An export of Settings (`SettingsExport` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct SettingsExport {
    #[serde(rename = "type")]
    pub type_: String,
    #[serde(rename = "config")]
    pub config: Map<String, Value>,
}

/// A request to import Settings (`SettingsImportRequest` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct SettingsImportRequest {
    #[serde(flatten)]
    pub request: ImportRequest,
    #[serde(rename = "reset")]
    pub reset: bool,
    #[serde(rename = "allowUnknown")]
    pub allow_unknown: bool,
    #[serde(rename = "export")]
    pub export: Option<SettingsExport>,
}

/// An export of a single setting (`SingleSettingExport` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct SingleSettingExport {
    #[serde(rename = "type")]
    pub type_: String,
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "value")]
    pub value: Value,
}

/// A request to import a single setting (`SingleSettingImportRequest` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct SingleSettingImportRequest {
    #[serde(flatten)]
    pub request: ImportRequest,
    #[serde(rename = "export")]
    pub export: Option<SingleSettingExport>,
}

/// A fingerprint in an exported profile (`ProfileFingerprint` in Go).
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct ProfileFingerprint {
    #[serde(rename = "type")]
    pub type_: String,
    #[serde(rename = "key", skip_serializing_if = "String::is_empty")]
    pub key: String,
    #[serde(rename = "operation")]
    pub operation: String,
    #[serde(rename = "value")]
    pub value: String,
    #[serde(rename = "mergedFrom", skip_serializing_if = "String::is_empty")]
    pub merged_from: String,
}

/// Profile icon only (`ProfileIcon` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ProfileIcon {
    #[serde(rename = "iconData", skip_serializing_if = "String::is_empty")]
    pub icon_data: String,
}

/// An export of an application profile (`ProfileExport` in Go).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct ProfileExport {
    #[serde(rename = "type")]
    pub type_: String,

    // Identification.
    #[serde(rename = "id", skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(rename = "source", skip_serializing_if = "String::is_empty")]
    pub source: String,

    // Human metadata.
    #[serde(rename = "name")]
    pub name: String,
    #[serde(rename = "description", skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(rename = "homepage", skip_serializing_if = "String::is_empty")]
    pub homepage: String,
    #[serde(rename = "presPath", skip_serializing_if = "String::is_empty")]
    pub presentation_path: String,
    #[serde(rename = "usePresPath", skip_serializing_if = "is_false")]
    pub use_presentation_path: bool,
    #[serde(rename = "iconData", skip_serializing_if = "String::is_empty")]
    pub icon_data: String,

    // Process matching.
    #[serde(rename = "fingerprints")]
    pub fingerprints: Vec<ProfileFingerprint>,

    // Settings.
    #[serde(rename = "config", skip_serializing_if = "Map::is_empty")]
    pub config: Map<String, Value>,

    // Metadata. RFC3339 strings, like Go's `*time.Time`.
    #[serde(rename = "lastEdited", skip_serializing_if = "Option::is_none")]
    pub last_edited: Option<String>,
    #[serde(rename = "created", skip_serializing_if = "Option::is_none")]
    pub created: Option<String>,
    #[serde(rename = "internal", skip_serializing_if = "is_false")]
    pub internal: bool,
}

impl Default for ProfileExport {
    fn default() -> Self {
        ProfileExport {
            type_: String::new(),
            id: String::new(),
            source: String::new(),
            name: String::new(),
            description: String::new(),
            homepage: String::new(),
            presentation_path: String::new(),
            use_presentation_path: false,
            icon_data: String::new(),
            fingerprints: Vec::new(),
            config: Map::new(),
            last_edited: None,
            created: None,
            internal: false,
        }
    }
}

/// Request for a profile export (`ProfileExportRequest` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ProfileExportRequest {
    #[serde(rename = "id")]
    pub id: String,
}

/// Request to import a profile (`ProfileImportRequest` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ProfileImportRequest {
    #[serde(flatten)]
    pub request: ImportRequest,
    #[serde(rename = "allowUnknown")]
    pub allow_unknown: bool,
    #[serde(rename = "allowReplaceProfiles")]
    pub allow_replace: bool,
    #[serde(rename = "export")]
    pub export: Option<ProfileExport>,
}

/// Result of a profile import (`ProfileImportResult` in Go).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct ProfileImportResult {
    #[serde(flatten)]
    pub result: ImportResult,
    #[serde(rename = "replacesProfiles")]
    pub replaces_profiles: Vec<String>,
}
