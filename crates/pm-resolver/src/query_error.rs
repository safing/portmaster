//! The error values of Go `resolve.go`, with `errors.Is`-style helpers.

use crate::rrcache::RRCache;

/// Why a query could not be answered.
///
/// Go matches errors with `errors.Is`; the helpers [`QueryError::is_not_found`]
/// and [`QueryError::is_blocked`] follow the same wrapping rules.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum QueryError {
    /// Matches all "not found" errors.
    #[error("record could not be found")]
    NotFound,
    /// Matches all "blocked" errors.
    #[error("query was blocked")]
    Blocked,
    /// Returned to `*.localhost` queries.
    #[error("query for localhost")]
    Localhost,
    #[error("query timed out")]
    Timeout,
    #[error("device is offine")]
    Offline,
    #[error("query failed")]
    Failure,
    #[error("resolver has no answer")]
    Continue,
    #[error("resolver is shutting down")]
    ShuttingDown,
    #[error("query was cancelled")]
    Cancelled,
    /// Wraps `Blocked`.
    #[error("query was blocked: test domains disabled")]
    TestDomainsDisabled,
    /// Wraps `Blocked`.
    #[error("query was blocked: special domains disabled")]
    SpecialDomainsDisabled,
    /// Wraps `NotFound`.
    #[error("record could not be found: invalid request")]
    Invalid,
    /// Wraps `Blocked`: no resolver was able to comply with the settings.
    #[error("query was blocked: no compliant resolvers for this query")]
    NoCompliance,
    /// The query was blocked by the upstream resolver; wraps `Blocked`.
    #[error("query was blocked by upstream DNS resolver {resolver_name}")]
    BlockedUpstream { resolver_name: String },
    /// All compliant resolvers failed; wraps the last error.
    #[error("all {count} query-compliant resolvers failed, last error: {last}")]
    AllFailed { count: usize, last: Box<QueryError> },
    /// Any other failure of a transport.
    #[error("{0}")]
    Other(String),
}

impl QueryError {
    /// Like `errors.Is(err, ErrNotFound)`.
    pub fn is_not_found(&self) -> bool {
        match self {
            QueryError::NotFound | QueryError::Invalid => true,
            QueryError::AllFailed { last, .. } => last.is_not_found(),
            _ => false,
        }
    }

    /// Like `errors.Is(err, ErrBlocked)`.
    pub fn is_blocked(&self) -> bool {
        match self {
            QueryError::Blocked
            | QueryError::TestDomainsDisabled
            | QueryError::SpecialDomainsDisabled
            | QueryError::NoCompliance
            | QueryError::BlockedUpstream { .. } => true,
            QueryError::AllFailed { last, .. } => last.is_blocked(),
            _ => false,
        }
    }
}

/// The outcome of a failed resolve: the error, and an expired cache entry that
/// may still be used (Go returns it alongside `ErrOffline`).
#[derive(Debug, Clone)]
pub struct ResolveFailure {
    pub error: QueryError,
    pub stale: Option<Box<RRCache>>,
}

impl ResolveFailure {
    pub(crate) fn new(error: QueryError) -> ResolveFailure {
        ResolveFailure { error, stale: None }
    }

    pub(crate) fn with_stale(error: QueryError, stale: Option<RRCache>) -> ResolveFailure {
        ResolveFailure {
            error,
            stale: stale.map(Box::new),
        }
    }
}

impl std::fmt::Display for ResolveFailure {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.error.fmt(f)
    }
}

impl std::error::Error for ResolveFailure {}

impl From<QueryError> for ResolveFailure {
    fn from(error: QueryError) -> Self {
        ResolveFailure::new(error)
    }
}
