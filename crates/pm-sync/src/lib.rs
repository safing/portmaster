//! Port of the Go `service/sync` module (without SPN; the package has no SPN
//! code to port).
//!
//! The crate implements the share-able export/import format for global
//! settings, single settings and application profiles, byte-compatible with
//! the Go implementation (JSON names, order and jess checksums), and exposes
//! the same six API endpoints with the same paths and permissions.

pub mod api;
mod format;
mod model;
mod sync;

pub use format::{format_from_accept, parse_export, serialize_export, serialize_profile_export};
pub use model::*;
pub use sync::Sync;

/// Profile source used by profile imports (`profile.SourceLocal` in Go).
pub const SOURCE_LOCAL: &str = pm_profile::SOURCE_LOCAL;

// Messages from Go `service/sync/util.go`.
pub const MSG_NONE: &str = "";
pub const MSG_VALID: &str = "Import is valid.";
pub const MSG_SUCCESS: &str = "Import successful.";
pub const MSG_REQUIRE_RESTART: &str =
    "Import successful. Restart required for setting to take effect.";

/// An error carrying the HTTP status Go's `api.ErrorWithStatus` would assign.
#[derive(Debug, thiserror::Error)]
#[error("{message}")]
pub struct SyncError {
    pub status: u16,
    pub message: String,
}

impl SyncError {
    pub fn new(status: u16, message: impl Into<String>) -> SyncError {
        SyncError {
            status,
            message: message.into(),
        }
    }

    /// Wraps this error with a Go-style `%w: <message>` prefix, keeping status.
    pub fn wrap(&self, prefix: &str) -> SyncError {
        SyncError::new(self.status, format!("{prefix}: {}", self.message))
    }

    pub fn api(self) -> pm_api::ApiError {
        pm_api::ApiError::WithStatus {
            status: self.status,
            message: self.message,
        }
    }
}

/// Result type of this crate.
pub type Result<T> = std::result::Result<T, SyncError>;

/// "the supplied export cannot be imported here" (HTTP 412).
pub fn err_mismatch() -> SyncError {
    SyncError::new(412, "the supplied export cannot be imported here")
}
/// "setting not found" (HTTP 412).
pub fn err_setting_not_found() -> SyncError {
    SyncError::new(412, "setting not found")
}
/// "import/export target does not exist" (HTTP 410).
pub fn err_target_not_found() -> SyncError {
    SyncError::new(410, "import/export target does not exist")
}
/// "cannot export unchanged setting" (HTTP 410).
pub fn err_unchanged() -> SyncError {
    SyncError::new(410, "cannot export unchanged setting")
}
/// "cannot be set per app" (HTTP 410).
pub fn err_not_settable_per_app() -> SyncError {
    SyncError::new(410, "cannot be set per app")
}
/// "invalid import request" (HTTP 422).
pub fn err_invalid_import_request() -> SyncError {
    SyncError::new(422, "invalid import request")
}
/// "invalid setting value" (HTTP 422).
pub fn err_invalid_setting_value() -> SyncError {
    SyncError::new(422, "invalid setting value")
}
/// "invalid profile data" (HTTP 422).
pub fn err_invalid_profile_data() -> SyncError {
    SyncError::new(422, "invalid profile data")
}
/// "import failed" (HTTP 500).
pub fn err_import_failed() -> SyncError {
    SyncError::new(500, "import failed")
}
/// "export failed" (HTTP 500).
pub fn err_export_failed() -> SyncError {
    SyncError::new(500, "export failed")
}

/// Error of the DSD layer, as reported for an incompatible format.
pub fn err_incompatible_format() -> SyncError {
    SyncError::new(500, "dsd: format is incompatible with operation")
}
