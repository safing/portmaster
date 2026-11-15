//! Errors of the database system, with the messages of the Go implementation.

/// Result type of this crate.
pub type Result<T> = std::result::Result<T, Error>;

/// Errors returned by the database, the controller and the storages.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("database entry not found")]
    NotFound,
    #[error("access to database record denied")]
    PermissionDenied,
    #[error("database is read only")]
    ReadOnly,
    #[error("database system is shutting down")]
    ShuttingDown,
    #[error("not implemented by this storage")]
    NotImplemented,
    #[error("storage entry not found")]
    StorageNotFound,
    #[error("record is malformed")]
    RecordMalformed,
    #[error("incompatible serialization format")]
    IncompatibleFormat,
    #[error("database \"{0}\" not registered")]
    NotRegistered(String),
    #[error("database \"{0}\" already loaded")]
    AlreadyLoaded(String),
    #[error("invalid database name: {0}")]
    InvalidName(String),
    #[error("database storage is not injected")]
    NotInjected,
    #[error("database not of type \"injected\"")]
    NotInjectedType,
    #[error("sqlite: {0}")]
    Sqlite(#[from] rusqlite::Error),
    #[error("record: {0}")]
    Record(#[from] pm_record::Error),
    #[error("query: {0}")]
    Query(#[from] pm_query::Error),
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("{0}")]
    Other(String),
}
