//! Parsing of varlink reply messages of the systemd-resolved DNS monitor.

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::varlinktypes::QueryResult;

/// One varlink reply message. `parameters` carries the method's return value
/// (a [`QueryResult`] for `SubscribeQueryResults`), `continues` is the varlink
/// `more` flag, and `error` is set when the daemon returned a varlink error.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct VarlinkEvent {
    #[serde(default)]
    pub parameters: QueryResult,
    #[serde(default)]
    pub continues: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<Value>,
}

impl VarlinkEvent {
    /// Returns the varlink error parameters, if the daemon reported an error.
    pub fn varlink_error(&self) -> Option<String> {
        self.error.as_ref().map(|error| error.to_string())
    }
}

/// Error of parsing a varlink event.
#[derive(Debug, thiserror::Error)]
pub enum EventParseError {
    #[error("empty varlink event")]
    Empty,
    #[error("invalid varlink event: {0}")]
    Json(#[from] serde_json::Error),
}

/// Parses one raw varlink message.
///
/// Both the varlink reply envelope (`{"parameters": ..., "continues": ...}`)
/// and a bare `QueryResult` object are accepted; a bare object is treated as a
/// message that continues the stream. This keeps the parser usable with the
/// raw method replies as well as with the decoded `QueryResult` of the Go
/// `receive` callback.
pub fn parse_event(line: &str) -> Result<VarlinkEvent, EventParseError> {
    if line.trim().is_empty() {
        return Err(EventParseError::Empty);
    }

    let value: Value = serde_json::from_str(line)?;
    if value.get("parameters").is_some() || value.get("error").is_some() {
        return Ok(serde_json::from_value(value)?);
    }

    let parameters: QueryResult = serde_json::from_value(value)?;
    Ok(VarlinkEvent {
        parameters,
        continues: true,
        error: None,
    })
}

/// Parses just the parameters of a varlink message, like the Go `receive`
/// callback unmarshalling into `QueryResult`.
pub fn parse_query_result(line: &str) -> Result<QueryResult, EventParseError> {
    let value: Value = serde_json::from_str(line)?;
    Ok(serde_json::from_value(
        value
            .get("parameters")
            .cloned()
            .unwrap_or(value),
    )?)
}
