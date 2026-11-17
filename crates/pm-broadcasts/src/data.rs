//! Matching data collection, ported from Go `service/broadcasts/data.go`.
//!
//! Go calls into four other services (`service/core` versions,
//! `base/config`, `service/netenv` location and the install info record). The
//! [`MatchingEnvironment`] trait is the explicit adapter for those services:
//! the core implements it with its own modules, tests inject deterministic
//! values. This is a real seam, not a stub — every value Go exposes is still
//! produced, only the source is abstracted.

use std::path::PathBuf;

use serde::Serialize;
use serde_json::{json, Value};

use crate::install_info::{make_numeric_version, InstallInfo};
use crate::{Broadcasts, Error};

/// Simplified view of Go `core.SimpleVersions`.
#[derive(Debug, Clone, Default, Serialize)]
pub struct SimpleVersions {
    #[serde(rename = "Build")]
    pub build: BuildVersion,
    #[serde(rename = "Resources")]
    pub resources: std::collections::BTreeMap<String, ResourceVersion>,
    #[serde(rename = "Channel")]
    pub channel: String,
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct BuildVersion {
    #[serde(rename = "Version")]
    pub version: String,
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct ResourceVersion {
    #[serde(rename = "Version")]
    pub version: String,
}

/// Matching data describing the device location.
#[derive(Debug, Clone, Default, Serialize)]
pub struct LocationData {
    #[serde(rename = "Country")]
    pub country: String,
    #[serde(rename = "Coordinates")]
    pub coordinates: CoordinatesData,
    #[serde(rename = "ASN")]
    pub asn: u64,
    #[serde(rename = "ASOrg")]
    pub as_org: String,
    #[serde(rename = "Source")]
    pub source: String,
    #[serde(rename = "SourceAccuracy")]
    pub source_accuracy: i32,
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct CoordinatesData {
    #[serde(rename = "AccuracyRadius")]
    pub accuracy_radius: u16,
    #[serde(rename = "Latitude")]
    pub latitude: f64,
    #[serde(rename = "Longitude")]
    pub longitude: f64,
}

/// Adapter for the Go services consulted while collecting matching data.
pub trait MatchingEnvironment: Send + Sync + 'static {
    /// Go `core.GetSimpleVersions()`.
    fn versions(&self) -> SimpleVersions;
    /// Go `config.GetActiveConfigValues()`.
    fn config_values(&self) -> serde_json::Map<String, Value>;
    /// Go `netenv.GetInternetLocation()` (best location only).
    fn location(&self) -> Option<LocationData>;
    /// Current wall-clock time as Unix seconds, like Go `time.Now()`.
    fn now(&self) -> i64;
    /// Local UTC offset in seconds, like Go `time.Now().Zone()`.
    fn local_offset_seconds(&self) -> i32;
}

/// Resolves an intel resource to a local file, like Go
/// `module.instance.IntelUpdates().GetFile(name).Path()`.
pub trait BroadcastFileSource: Send + Sync + 'static {
    fn get_file(&self, name: &str) -> Option<PathBuf>;
}

/// A file source rooted at a directory, used by tests and local installs.
pub struct DirectoryFileSource {
    root: PathBuf,
}

impl DirectoryFileSource {
    pub fn new(root: impl Into<PathBuf>) -> DirectoryFileSource {
        DirectoryFileSource { root: root.into() }
    }
}

impl BroadcastFileSource for DirectoryFileSource {
    fn get_file(&self, name: &str) -> Option<PathBuf> {
        let path = self.root.join(name);
        path.exists().then_some(path)
    }
}

/// Adapter for Go `Notification.SyncWithState(module.states)`.
///
/// Rust's `pm_status` derives notifications from states (the reverse
/// direction) and `pm_mgr::StateMgr` has no attach hook, so the notification
/// cannot literally follow a state's lifecycle. The core supplies an
/// implementation that records the attachment; it is intentionally a
/// visible seam instead of a silent no-op.
pub trait ModuleAttachment: Send + Sync + 'static {
    fn attach(&self, event_id: &str);
}

/// Default attachment implementation (used when no module states are wired).
pub struct NoopAttachment;

impl ModuleAttachment for NoopAttachment {
    fn attach(&self, _event_id: &str) {}
}

/// Date and time in the numeric formats Go uses in matching data.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
pub struct DateTime {
    #[serde(rename = "NumericDateTime")]
    pub numeric_date_time: i64,
    #[serde(rename = "NumericDate")]
    pub numeric_date: i64,
    #[serde(rename = "NumericTime")]
    pub numeric_time: i64,
}

fn make_date_time_info(unix_seconds: i64) -> DateTime {
    // pm_dsd::gotime formats UTC as YYYY-MM-DDTHH:MM:SSZ; the numeric forms
    // are the same digits without separators.
    let text = pm_dsd::gotime::format_rfc3339(unix_seconds.max(1));
    let bytes = text.as_bytes();
    let digits: String = bytes
        .iter()
        .copied()
        .filter(u8::is_ascii_digit)
        .map(char::from)
        .collect();
    if digits.len() < 14 {
        return DateTime::default();
    }
    DateTime {
        numeric_date_time: digits[..14].parse().unwrap_or(0),
        numeric_date: digits[..8].parse().unwrap_or(0),
        numeric_time: digits[8..14].parse().unwrap_or(0),
    }
}

impl Broadcasts {
    /// Collects the JSON matching data exposed by
    /// `broadcasts/matching-data` and used for matching, like Go `collectData`.
    pub fn collect_data(&self) -> Value {
        let now = self.env.now();
        let versions = self.env.versions();
        let mut data = serde_json::Map::new();

        // Versions.
        let updates = serde_json::to_value(&versions).unwrap_or(Value::Null);
        data.insert("Updates".to_string(), updates);
        data.insert(
            "Version".to_string(),
            Value::String(versions.build.version.clone()),
        );
        match make_numeric_version(&versions.build.version) {
            Ok(numeric) => {
                data.insert("NumericVersion".to_string(), json!(numeric));
            }
            Err(error) => {
                data.insert("NumericVersion".to_string(), data_error(&error));
            }
        }

        // Install info.
        match InstallInfo::get(&self.db) {
            Ok(install_info) => match serde_json::to_value(&install_info) {
                Ok(value) => {
                    data.insert("Install".to_string(), value);
                }
                Err(error) => {
                    data.insert("Install".to_string(), data_error(&Error::Json(error)));
                }
            },
            Err(Error::Database(pm_database::Error::NotFound)) => {
                // Go wraps even database.ErrNotFound in a DataError. A plain
                // error has no exported fields, so it marshals as `{}`.
                data.insert("Install".to_string(), json!({ "Error": {} }));
            }
            Err(error) => {
                data.insert("Install".to_string(), data_error(&error));
            }
        }

        // Global configuration.
        data.insert(
            "Config".to_string(),
            Value::Object(self.env.config_values()),
        );

        // Device location.
        if let Some(location) = self.env.location() {
            match serde_json::to_value(&location) {
                Ok(value) => {
                    data.insert("Location".to_string(), value);
                }
                Err(error) => {
                    data.insert("Location".to_string(), data_error(&Error::Json(error)));
                }
            }
        }

        // Time running and current date/time.
        let started_at = *self
            .started_at
            .lock()
            .unwrap_or_else(|error| error.into_inner());
        data.insert(
            "UptimeHours".to_string(),
            json!((now - started_at).max(0) / 3600),
        );
        let offset = i64::from(self.env.local_offset_seconds());
        data.insert(
            "Current".to_string(),
            json!({
                "UnixTime": now,
                "UTC": make_date_time_info(now),
                "Local": make_date_time_info(now + offset),
            }),
        );

        Value::Object(data)
    }
}

fn data_error(_error: &Error) -> Value {
    // Go's `DataError{Error: err}` marshals the `error` interface value by its
    // concrete type. Plain errors (e.g. `*errors.errorString`) have no exported
    // fields, so the result is an empty object rather than the error message.
    json!({ "Error": {} })
}
