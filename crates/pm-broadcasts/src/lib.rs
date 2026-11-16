//! Port of Go `service/broadcasts`.
//!
//! Broadcast notifications are YAML defined, matched against local instance
//! data with the `base/database/query` language, acknowledged by the user and
//! remembered in `core:broadcasts/state`. The public surface is intentionally
//! small: [`Broadcasts`] plus the adapter traits in [`data`].

mod api;
mod data;
mod handler;
mod install_info;
mod module;
mod source;
mod state;

pub use data::{
    BroadcastFileSource, CoordinatesData, DateTime, DirectoryFileSource, LocationData,
    MatchingEnvironment, ModuleAttachment, NoopAttachment, ResourceVersion, SimpleVersions,
    BuildVersion,
};
pub use handler::{HandleResult, SkipReason};
pub use install_info::{format_numeric_date, make_numeric_version, InstallInfo, INSTALL_INFO_DB_KEY};
pub use module::Broadcasts;
pub use source::{
    parse_broadcast_source, parse_duration, BroadcastNotification, BroadcastNotifications,
    BROADCAST_NOTIFICATION_ID_PREFIX, BROADCASTS_RESOURCE_NAME, MIN_REPEAT_DURATION,
};
pub use state::{BroadcastState, BroadcastStates, BROADCAST_STATES_DB_KEY};

/// Errors returned by the broadcasts module.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("{0}")]
    Message(String),
    #[error("database: {0}")]
    Database(#[from] pm_database::Error),
    #[error("record: {0}")]
    Record(#[from] pm_record::Error),
    #[error("yaml: {0}")]
    Yaml(#[from] serde_yaml::Error),
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),
}

/// Result type of the broadcasts module.
pub type Result<T> = std::result::Result<T, Error>;
