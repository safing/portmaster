use thiserror::Error;

/// MessageError describes any error that is encountered when parsing
/// PortAPI messages or when converting between the Request/Response types.
#[derive(Debug, Error)]
pub enum MessageError {
    #[error("missing command id")]
    MissingID,

    #[error("invalid command id")]
    InvalidID,

    #[error("missing command")]
    MissingCommand,

    #[error("missing key")]
    MissingKey,

    #[error("missing payload")]
    MissingPayload,

    #[error("unknown or unsupported command: {0}")]
    UnknownCommand(String),

    #[error(transparent)]
    InvalidPayload(#[from] serde_json::Error),
}


/// Payload defines the payload type and content of a PortAPI message.
/// 
/// For the time being, only JSON payloads (indicated by a prefixed 'J' of the payload content)
/// is directly supported in `Payload::parse()`.
/// 
/// For other payload types (like CBOR, BSON, ...) it's the user responsibility to figure out
/// appropriate decoding from the `Payload::UNKNOWN` variant.
#[derive(PartialEq, Debug, Clone)]
pub enum Payload {
    JSON(String),
    UNKNOWN(String),
}

/// ParseError is returned from `Payload::parse()`.
#[derive(Debug, Error)]
pub enum ParseError {
    #[error(transparent)]
    JSON(#[from] serde_json::Error),

    #[error("unknown error while parsing")]
    UNKNOWN
}


impl Payload {
    /// Parse the payload into T.
    /// 
    /// Only JSON parsing is supported for now. See [Payload] for more information.
    pub fn parse<'a, T>(self: &'a Self) -> std::result::Result<T, ParseError> 
    where
        T: serde::de::Deserialize<'a> {

        match self {
            Payload::JSON(blob) => Ok(serde_json::from_str::<T>(blob.as_str())?),
            Payload::UNKNOWN(_) => Err(ParseError::UNKNOWN),
        }
    }
}

/// Supports creating a Payload instance from a String.
/// 
/// See [Payload] for more information.
impl std::convert::From<String> for Payload {
    fn from(value: String) -> Payload {
        let mut chars = value.chars();
        let first = chars.next();
        let rest = chars.as_str().to_string();

        match first {
            Some(c) => match c {
                'J' => Payload::JSON(rest),
                _ => Payload::UNKNOWN(value),
            },
            None => Payload::UNKNOWN("".to_string())
        }
    }
}

/// Display implementation for Payload that just displays the raw payload.
impl std::fmt::Display for Payload {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Payload::JSON(payload) => {
                write!(f, "J{}", payload)
            },
            Payload::UNKNOWN(payload) => {
                write!(f, "{}", payload)
            }
        }
    }
}

/// Message is an internal representation of a PortAPI message.
/// Users should more likely use `portapi::types::Request` and `portapi::types::Response` 
/// instead of directly using `Message`.
/// 
/// The struct is still public since it might be useful for debugging or to implement new
/// commands not yet supported by the `portapi::types` crate.
#[derive(PartialEq, Debug, Clone)]
pub struct Message {
    pub id: usize,
    pub cmd: String,
    pub key: Option<String>,
    pub payload: Option<Payload>,
}

/// Implementation to marshal a PortAPI message into it's wire-format representation
/// (which is a string).
/// 
/// Note that this conversion does not check for invalid messages!
impl std::convert::From<Message> for String {
    fn from(value: Message) -> Self {
        let mut result = "".to_owned();

        result.push_str(value.id.to_string().as_str());
        result.push_str("|");
        result.push_str(&value.cmd);

        if let Some(key) = value.key {
            result.push_str("|");
            result.push_str(key.as_str());
        }

        if let Some(payload) = value.payload {
            result.push_str("|");
            result.push_str(payload.to_string().as_str())
        }

        result
    }
}

/// An implementation for `String::parse()` to convert a wire-format representation
/// of a PortAPI message to a Message instance.
/// 
/// Any errors returned from `String::parse()` will be of type `MessageError`
impl std::str::FromStr for Message {
    type Err = MessageError;

    fn from_str(line: &str) -> Result<Self, Self::Err> {
        let parts = line.split("|").collect::<Vec<&str>>();

        let id = match parts.get(0) {
            Some(s) => match (*s).parse::<usize>() {
                Ok(id) => Ok(id),
                Err(_) => Err(MessageError::InvalidID),
            },
            None => Err(MessageError::MissingID),
        }?;

        let cmd = match parts.get(1) {
            Some(s) => Ok(*s),
            None => Err(MessageError::MissingCommand),
        }?
        .to_string();

        let key = parts.get(2)
            .and_then(|key| Some(key.to_string()));

        let payload : Option<Payload> = parts.get(3)
            .and_then(|p| Some(p.to_string().into()));

        return Ok(Message {
            id,
            cmd,
            key,
            payload: payload
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Deserialize;

    #[derive(Debug, PartialEq, Deserialize)]
    struct Test {
        a: i64,
        s: String,
    }

    #[test]
    fn payload_to_string() {
        let p = Payload::JSON("{}".to_string());
        assert_eq!(p.to_string(), "J{}");

        let p = Payload::UNKNOWN("some unknown content".to_string());
        assert_eq!(p.to_string(), "some unknown content");
    }

    #[test]
    fn payload_from_string() {
        let p: Payload = "J{}".to_string().into();
        assert_eq!(p, Payload::JSON("{}".to_string()));

        let p: Payload = "some unknown content".to_string().into();
        assert_eq!(p, Payload::UNKNOWN("some unknown content".to_string()));
    }

    #[test]
    fn payload_parse() {
        let p: Payload = "J{\"a\": 100, \"s\": \"string\"}".to_string().into();

        let t: Test = p.parse()
            .expect("Expected payload parsing to work");

        assert_eq!(t, Test{
            a: 100,
            s: "string".to_string(),
        });
    }

    #[test]
    fn parse_message() {
        let m = "10|insert|some:key|J{}".parse::<Message>()
            .expect("Expected message to parse");

        assert_eq!(m, Message{
            id: 10,
            cmd: "insert".to_string(),
            key: Some("some:key".to_string()),
            payload: Some(Payload::JSON("{}".to_string())),
        });

        let m = "1|done".parse::<Message>()
            .expect("Expected message to parse");

        assert_eq!(m, Message{
            id: 1,
            cmd: "done".to_string(),
            key: None,
            payload: None
        });

        let m = "".parse::<Message>()
            .expect_err("Expected parsing to fail");
        if let MessageError::InvalidID = m {} else {
            panic!("unexpected error value: {}", m)
        }

        let m = "1".parse::<Message>()
            .expect_err("Expected parsing to fail");

        if let MessageError::MissingCommand = m {} else {
            panic!("unexpected error value: {}", m)
        }
    }
}
