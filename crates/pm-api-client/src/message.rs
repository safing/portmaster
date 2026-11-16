//! Wire messages of the database WebSocket API.
//!
//! Frames are `opID|type|key|data`, split like Go's `bytes.SplitN(msg, "|", 4)`.
//! Record data carries the DSD JSON format byte (`J`) followed by the JSON
//! payload, exactly like [`pm_api::DatabaseApi`](../pm_api/struct.DatabaseApi.html)
//! produces it.

use serde::de::DeserializeOwned;
use serde_json::Value;

use crate::error::{Error, Result};

/// Message type identifiers used on the wire.
pub mod msg {
    // Requests.
    pub const GET: &str = "get";
    pub const QUERY: &str = "query";
    pub const SUB: &str = "sub";
    pub const QSUB: &str = "qsub";
    pub const CREATE: &str = "create";
    pub const UPDATE: &str = "update";
    pub const INSERT: &str = "insert";
    pub const DELETE: &str = "delete";
    pub const CANCEL: &str = "cancel";

    // Replies (exported by the Go client as `Msg*`).
    pub const OK: &str = "ok";
    pub const ERROR: &str = "error";
    pub const DONE: &str = "done";
    pub const SUCCESS: &str = "success";
    pub const UPDATE_RECORD: &str = "upd";
    pub const NEW: &str = "new";
    pub const DEL: &str = "del";
    pub const WARNING: &str = "warning";

    /// Special message type delivered to operations when the connection is lost.
    pub const OFFLINE: &str = "offline";
}

/// Separator between frame fields.
pub const SEPARATOR: u8 = b'|';

/// One API message, either a request to pack or a parsed reply.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Message {
    /// Identifier of the operation this message belongs to.
    pub op_id: String,
    /// Message type, one of the [`msg`] constants.
    pub msg_type: String,
    /// Record key, query text or error message.
    pub key: String,
    /// Raw DSD payload (format byte + serialized value) of a record frame.
    pub raw_value: Vec<u8>,
    /// Value to serialize when packing a request.
    pub value: Option<Value>,
}

impl Message {
    /// Creates a message with the given operation id and type.
    pub fn new(op_id: impl Into<String>, msg_type: impl Into<String>) -> Message {
        Message {
            op_id: op_id.into(),
            msg_type: msg_type.into(),
            key: String::new(),
            raw_value: Vec::new(),
            value: None,
        }
    }

    /// Creates a request message with an optional JSON value.
    pub fn request(
        op_id: impl Into<String>,
        msg_type: impl Into<String>,
        key: impl Into<String>,
        value: Option<Value>,
    ) -> Message {
        Message {
            key: key.into(),
            value,
            ..Message::new(op_id, msg_type)
        }
    }

    /// Creates the special offline notification.
    pub fn offline(op_id: impl Into<String>) -> Message {
        Message::new(op_id, msg::OFFLINE)
    }

    /// Parses one raw frame.
    ///
    /// Mirrors Go `ParseMessage`: record frames (`ok`/`upd`/`new`) need four
    /// fields, `del`/`error`/`warning` need three, `done`/`success` need two.
    pub fn parse(data: &[u8]) -> Result<Message> {
        let parts: Vec<&[u8]> = data.splitn(4, |&byte| byte == SEPARATOR).collect();
        if parts.len() < 2 {
            return Err(Error::MalformedMessage);
        }

        let op_id = String::from_utf8_lossy(parts[0]).into_owned();
        let msg_type = String::from_utf8_lossy(parts[1]).into_owned();
        let mut message = Message::new(op_id, msg_type);

        match message.msg_type.as_str() {
            msg::OK | msg::UPDATE_RECORD | msg::NEW => {
                if parts.len() != 4 {
                    return Err(Error::MalformedMessage);
                }
                message.key = String::from_utf8_lossy(parts[2]).into_owned();
                message.raw_value = parts[3].to_vec();
            }
            msg::DEL | msg::WARNING | msg::ERROR => {
                if parts.len() != 3 {
                    return Err(Error::MalformedMessage);
                }
                message.key = String::from_utf8_lossy(parts[2]).into_owned();
            }
            msg::DONE | msg::SUCCESS => {}
            _ => {}
        }

        Ok(message)
    }

    /// Serializes this message into a WebSocket frame.
    ///
    /// A value is dumped with the DSD JSON format byte (`J`) prefix, just like
    /// Go `dsd.Dump(value, dsd.JSON)`.
    pub fn pack(&self) -> Result<Vec<u8>> {
        let mut out = Vec::with_capacity(64);
        out.extend_from_slice(self.op_id.as_bytes());
        out.push(SEPARATOR);
        out.extend_from_slice(self.msg_type.as_bytes());

        if self.key.is_empty() {
            return Ok(out);
        }

        out.push(SEPARATOR);
        out.extend_from_slice(self.key.as_bytes());

        let raw = if !self.raw_value.is_empty() {
            self.raw_value.clone()
        } else if let Some(value) = &self.value {
            pm_dsd::dump(value, pm_dsd::format::JSON)?
        } else {
            Vec::new()
        };

        if !raw.is_empty() {
            out.push(SEPARATOR);
            out.extend_from_slice(&raw);
        }

        Ok(out)
    }

    /// Deserializes the DSD payload into a typed value.
    pub fn value<T: DeserializeOwned>(&self) -> Result<T> {
        let (value, _format) = pm_dsd::load(&self.raw_value)?;
        Ok(value)
    }

    /// Deserializes the DSD payload into a JSON value, if present.
    pub fn value_json(&self) -> Result<Value> {
        self.value()
    }

    /// Returns whether this is a record frame (`ok`/`upd`/`new`).
    pub fn is_record(&self) -> bool {
        matches!(
            self.msg_type.as_str(),
            msg::OK | msg::UPDATE_RECORD | msg::NEW
        )
    }

    /// Returns whether this operation finished (`done` or `success`).
    pub fn is_done(&self) -> bool {
        matches!(self.msg_type.as_str(), msg::DONE | msg::SUCCESS)
    }

    /// Returns whether this is an error or warning frame.
    pub fn is_error(&self) -> bool {
        matches!(self.msg_type.as_str(), msg::ERROR | msg::WARNING)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn pack_request_with_value_has_dsd_json_byte() {
        let message = Message::request(
            "#1",
            msg::CREATE,
            "test:key",
            Some(json!({"Value": 42})),
        );
        let packed = message.pack().unwrap();
        assert_eq!(&packed, b"#1|create|test:key|J{\"Value\":42}");
    }

    #[test]
    fn pack_request_without_value() {
        let packed = Message::request("#2", msg::GET, "test:key", None)
            .pack()
            .unwrap();
        assert_eq!(&packed, b"#2|get|test:key");
        assert_eq!(
            Message::request("#3", msg::CANCEL, "", None).pack().unwrap(),
            b"#3|cancel"
        );
    }

    #[test]
    fn pack_uses_raw_value_over_value() {
        let mut message = Message::request("#4", msg::UPDATE, "test:key", Some(json!(1)));
        message.raw_value = b"J{\"Value\":9}".to_vec();
        assert_eq!(&message.pack().unwrap(), b"#4|update|test:key|J{\"Value\":9}");
    }

    #[test]
    fn parse_record_frames() {
        let message = Message::parse(b"127|ok|test:key|J{\"Value\":42}").unwrap();
        assert_eq!(message.op_id, "127");
        assert_eq!(message.msg_type, msg::OK);
        assert_eq!(message.key, "test:key");
        assert_eq!(message.raw_value, b"J{\"Value\":42}");
        assert!(message.is_record());
        assert_eq!(message.value_json().unwrap()["Value"], 42);
    }

    #[test]
    fn parse_simple_frames() {
        let error = Message::parse(b"127|error|database entry not found").unwrap();
        assert_eq!(error.msg_type, msg::ERROR);
        assert_eq!(error.key, "database entry not found");
        assert!(error.is_error());

        let deleted = Message::parse(b"127|del|test:key").unwrap();
        assert_eq!(deleted.msg_type, msg::DEL);
        assert_eq!(deleted.key, "test:key");

        let done = Message::parse(b"127|done").unwrap();
        assert!(done.is_done());
        let success = Message::parse(b"127|success").unwrap();
        assert!(success.is_done());
    }

    #[test]
    fn malformed_frames_are_rejected() {
        assert!(Message::parse(b"garbage").is_err());
        assert!(Message::parse(b"127|ok|only-key").is_err());
        assert!(Message::parse(b"127|del").is_err());
        assert!(Message::parse(b"127|error").is_err());
        // Unknown types are accepted, like the Go implementation.
        assert!(Message::parse(b"127|whatever|payload").is_ok());
    }
}
