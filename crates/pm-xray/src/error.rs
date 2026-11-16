//! Errors of the Xray integration.

/// Errors returned by the Xray crate.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("database: {0}")]
    Database(#[from] pm_database::Error),
    #[error("record: {0}")]
    Record(#[from] pm_record::Error),
    #[error("profile: {0}")]
    Parse(#[from] crate::parser::ParseError),
    #[error("subscription fetch: {0}")]
    Fetch(String),
    #[error("subscription not found: {0}")]
    SubscriptionNotFound(String),
    #[error("node not found: {0}")]
    NodeNotFound(String),
    #[error("config: {0}")]
    Config(String),
    #[error("process: {0}")]
    Process(String),
    #[error("xray is disabled")]
    Disabled,
    #[error("invalid request: {0}")]
    InvalidRequest(String),
}

/// Result type of the Xray crate.
pub type Result<T> = std::result::Result<T, Error>;

impl Error {
    /// Converts to an API error; 404 for unknown objects, 400 for bad input.
    pub fn api(self) -> pm_api::ApiError {
        use pm_api::ApiError;
        match self {
            Error::SubscriptionNotFound(id) => {
                ApiError::WithStatus { status: 404, message: format!("subscription not found: {id}") }
            }
            Error::NodeNotFound(id) => {
                ApiError::WithStatus { status: 404, message: format!("node not found: {id}") }
            }
            Error::InvalidRequest(message) => ApiError::WithStatus { status: 400, message },
            Error::Disabled => ApiError::WithStatus {
                status: 409,
                message: "xray is disabled".to_string(),
            },
            other => ApiError::Message(other.to_string()),
        }
    }
}
