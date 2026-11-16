//! Errors of the API client.

/// An error of the API client.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// A received frame did not follow the `op|type|key|data` protocol.
    #[error("malformed message")]
    MalformedMessage,

    /// The WebSocket layer failed (connecting, reading or writing).
    #[error("websocket: {0}")]
    WebSocket(String),

    /// A request header could not be constructed.
    #[error("invalid header: {0}")]
    InvalidHeader(String),

    /// DSD serialization/deserialization failed.
    #[error("dsd: {0}")]
    Dsd(#[from] pm_dsd::Error),

    /// JSON serialization/deserialization failed.
    #[error("json: {0}")]
    Json(#[from] serde_json::Error),

    /// The client was asked to connect while another connection is active.
    #[error("client is already connected")]
    AlreadyConnected,
}

/// Result type of this crate.
pub type Result<T> = std::result::Result<T, Error>;
