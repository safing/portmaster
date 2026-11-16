//! Errors of the metrics registry.

/// Errors returned by the metrics registry.
///
/// The messages mirror the Go `base/metrics` error values so callers can treat
/// them the same way.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    #[error("can only be changed before first metric is registered")]
    AlreadyStarted,
    #[error("metric already registered")]
    AlreadyRegistered,
    #[error("metric already registered with this internal ID")]
    AlreadyRegisteredInternalId,
    #[error("already set")]
    AlreadySet,
    #[error("invalid options: {0}")]
    InvalidOptions(String),
    #[error("already initialized")]
    AlreadyInitialized,
    #[error("api: {0}")]
    Api(String),
    #[error("registering metric {0:?} too early")]
    TooEarly(String),
}

pub type Result<T> = std::result::Result<T, Error>;
