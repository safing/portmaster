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
    Json(String),
    Unknown(String),
}

/// ParseError is returned from `Payload::parse()`.
#[derive(Debug, Error)]
pub enum ParseError {
    #[error(transparent)]
    Json(#[from] serde_json::Error),

    #[error("unknown error while parsing")]
    Unknown,
}

impl Payload {
    /// Parse the payload into T.
    ///
    /// Only JSON parsing is supported for now. See [Payload] for more information.
    pub fn parse<'a, T>(&'a self) -> std::result::Result<T, ParseError>
    where
        T: serde::de::Deserialize<'a>,
    {
        match self {
            Payload::Json(blob) => Ok(serde_json::from_str::<T>(blob.as_str())?),
            Payload::Unknown(_) => Err(ParseError::Unknown),
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
                'J' => Payload::Json(rest),
                _ => Payload::Unknown(value),
            },
            None => Payload::Unknown("".to_string()),
        }
    }
}

/// Display implementation for Payload that just displays the raw payload.
impl std::fmt::Display for Payload {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Payload::Json(payload) => {
                write!(f, "J{}", payload)
            }
            Payload::Unknown(payload) => {
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
        let mut result = String::new();

        result.push_str(value.id.to_string().as_str());
        result.push('|');
        result.push_str(&value.cmd);

        if let Some(key) = value.key {
            result.push('|');
            result.push_str(key.as_str());
        }

        if let Some(payload) = value.payload {
            result.push('|');
            result.push_str(payload.to_string().as_str())
        }

        result
    }
}

/// Reports whether messages of type `cmd` have the form `<id>|<cmd>|<key>|<payload>`.
///
/// For the other known types everything after `<id>|<cmd>|` is a single key,
/// query or message, which may contain '|' itself.
fn has_payload(cmd: &str) -> bool {
    match cmd {
        "del" | "error" | "warning" => false,
        "success" | "done" => false,
        "get" | "query" | "sub" | "qsub" | "delete" | "cancel" => false,
        // "ok", "upd", "new", "create", "update", "insert" and unknown commands.
        _ => true,
    }
}

/// An implementation for `String::parse()` to convert a wire-format representation
/// of a PortAPI message to a Message instance.
///
/// Any errors returned from `String::parse()` will be of type `MessageError`
impl std::str::FromStr for Message {
    type Err = MessageError;

    fn from_str(line: &str) -> Result<Self, Self::Err> {
        // Only split on the separators of the wire format: payloads (JSON),
        // queries and messages may contain '|' themselves. This mirrors the Go
        // side (base/api/database.go), which uses SplitN(msg, "|", 3) and, for
        // messages carrying a payload, splits the rest once more into <key>|<data>.
        // The command is always the second field.
        let cmd = line.split('|').nth(1).unwrap_or("");
        let limit = if has_payload(cmd) { 4 } else { 3 };
        let parts = line.splitn(limit, '|').collect::<Vec<&str>>();

        let id = match parts.first() {
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

        let key = parts.get(2).map(|key| key.to_string());
        let payload: Option<Payload> = parts.get(3).map(|p| p.to_string().into());

        Ok(Message {
            id,
            cmd,
            key,
            payload,
        })
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
        let p = Payload::Json("{}".to_string());
        assert_eq!(p.to_string(), "J{}");

        let p = Payload::Unknown("some unknown content".to_string());
        assert_eq!(p.to_string(), "some unknown content");
    }

    #[test]
    fn payload_from_string() {
        let p: Payload = "J{}".to_string().into();
        assert_eq!(p, Payload::Json("{}".to_string()));

        let p: Payload = "some unknown content".to_string().into();
        assert_eq!(p, Payload::Unknown("some unknown content".to_string()));
    }

    #[test]
    fn payload_parse() {
        let p: Payload = "J{\"a\": 100, \"s\": \"string\"}".to_string().into();

        let t: Test = p.parse().expect("Expected payload parsing to work");

        assert_eq!(
            t,
            Test {
                a: 100,
                s: "string".to_string(),
            }
        );
    }

    #[test]
    fn parse_message() {
        let m = "10|insert|some:key|J{}"
            .parse::<Message>()
            .expect("Expected message to parse");

        assert_eq!(
            m,
            Message {
                id: 10,
                cmd: "insert".to_string(),
                key: Some("some:key".to_string()),
                payload: Some(Payload::Json("{}".to_string())),
            }
        );

        let m = "1|done"
            .parse::<Message>()
            .expect("Expected message to parse");

        assert_eq!(
            m,
            Message {
                id: 1,
                cmd: "done".to_string(),
                key: None,
                payload: None
            }
        );

        let m = "".parse::<Message>().expect_err("Expected parsing to fail");
        if let MessageError::InvalidID = m {
        } else {
            panic!("unexpected error value: {}", m)
        }

        let m = "1"
            .parse::<Message>()
            .expect_err("Expected parsing to fail");

        if let MessageError::MissingCommand = m {
        } else {
            panic!("unexpected error value: {}", m)
        }
    }

    fn msg(id: usize, cmd: &str, key: Option<&str>, payload: Option<Payload>) -> Message {
        Message {
            id,
            cmd: cmd.to_string(),
            key: key.map(|k| k.to_string()),
            payload,
        }
    }

    fn json(s: &str) -> Option<Payload> {
        Some(Payload::Json(s.to_string()))
    }

    fn unknown(s: &str) -> Option<Payload> {
        Some(Payload::Unknown(s.to_string()))
    }

    #[test]
    fn parse_payload_containing_separator() {
        let data = r#"{"a": 1, "s": "x|y||z|"}"#;
        for cmd in ["ok", "upd", "new", "create", "update", "insert"] {
            let line = format!("7|{}|some:key|J{}", cmd, data);
            let m = line.parse::<Message>().expect("Expected message to parse");
            assert_eq!(m, msg(7, cmd, Some("some:key"), json(data)), "{}", line);

            // Marshaling yields the identical wire format again.
            assert_eq!(String::from(m), line);
        }

        let m = r#"3|ok|k|J{"a": 5, "s": "|"}"#.parse::<Message>();
        let p = m.expect("Expected message to parse").payload;
        let t: Test = p.unwrap().parse().expect("Expected payload parsing to work");
        let want = Test {
            a: 5,
            s: "|".to_string(),
        };
        assert_eq!(t, want);
    }

    #[test]
    fn parse_key_or_message_containing_separator() {
        let text = "a|b | c|";
        for cmd in ["del", "error", "warning", "get", "query", "sub", "qsub", "delete"] {
            let line = format!("9|{}|{}", cmd, text);
            let m = line.parse::<Message>().expect("Expected message to parse");
            assert_eq!(m, msg(9, cmd, Some(text), None), "{}", line);

            // Marshaling yields the identical wire format again.
            assert_eq!(String::from(m), line);
        }
    }

    #[test]
    fn parse_simple_messages_unchanged() {
        let k = Some("k");
        let cases = [
            ("1|ok|k|J{}", msg(1, "ok", k, json("{}"))),
            ("2|upd|k|J{}", msg(2, "upd", k, json("{}"))),
            ("3|new|k|J{}", msg(3, "new", k, json("{}"))),
            ("4|del|k", msg(4, "del", k, None)),
            ("5|success", msg(5, "success", None, None)),
            ("6|error|boom", msg(6, "error", Some("boom"), None)),
            ("7|warning|careful", msg(7, "warning", Some("careful"), None)),
            ("8|done", msg(8, "done", None, None)),
            ("9|get|k", msg(9, "get", k, None)),
            ("10|query|q", msg(10, "query", Some("q"), None)),
            ("11|sub|q", msg(11, "sub", Some("q"), None)),
            ("12|qsub|q", msg(12, "qsub", Some("q"), None)),
            ("13|create|k|J{}", msg(13, "create", k, json("{}"))),
            ("14|update|k|J{}", msg(14, "update", k, json("{}"))),
            ("15|insert|k|J{}", msg(15, "insert", k, json("{}"))),
            ("16|delete|k", msg(16, "delete", k, None)),
            ("17|cancel", msg(17, "cancel", None, None)),
            ("18|ok|k|xraw", msg(18, "ok", k, unknown("xraw"))),
            ("19|ok|k|", msg(19, "ok", k, unknown(""))),
            ("20|ok|k", msg(20, "ok", k, None)),
            ("21|ok", msg(21, "ok", None, None)),
            ("22|foo|k|p", msg(22, "foo", k, unknown("p"))),
            ("23|", msg(23, "", None, None)),
        ];

        for (line, want) in cases {
            let m = line.parse::<Message>().expect("Expected message to parse");
            assert_eq!(m, want, "{}", line);
        }

        let m = "x|ok|k|J{}".parse::<Message>();
        if let Err(MessageError::InvalidID) = m {
        } else {
            panic!("unexpected result: {:?}", m)
        }
    }

    #[test]
    fn response_with_separator_in_payload_or_message() {
        use super::super::types::Response;

        let m = r#"1|upd|core:status|J{"msg":"a|b"}"#.parse::<Message>();
        let r: Response = m.unwrap().try_into().unwrap();
        let want = Payload::Json(r#"{"msg":"a|b"}"#.to_string());
        assert_eq!(r, Response::Update("core:status".to_string(), want));

        let m = "2|error|bad request: x|y".parse::<Message>();
        let r: Response = m.unwrap().try_into().unwrap();
        assert_eq!(r, Response::Error("bad request: x|y".to_string()));
    }
}
