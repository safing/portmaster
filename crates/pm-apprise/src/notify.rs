//! Notification sending, ported from Go `base/apprise/notify.go`.

use std::sync::{Arc, RwLock};
use std::time::Duration;

use serde::{Deserialize, Serialize};
use serde_json::to_vec;

use crate::teaser::safe_first_16_bytes;

/// Default overall timeout used by the built-in HTTP client.
///
/// Matches the 10 second timeout used by the Go observation hub. Go's bare
/// `http.Client{}` has no timeout; a bounded one is used here on purpose so a
/// stalled Apprise server cannot hang the caller forever.
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(10);
/// Default connection timeout used by the built-in HTTP client.
pub const DEFAULT_CONNECT_TIMEOUT: Duration = Duration::from_secs(5);

/// Message type, a string as in Go (`type MsgType string`).
pub type MsgType = String;
/// Informational message type (`TypeInfo` in Go).
pub const TYPE_INFO: &str = "info";
/// Success message type (`TypeSuccess` in Go).
pub const TYPE_SUCCESS: &str = "success";
/// Warning message type (`TypeWarning` in Go).
pub const TYPE_WARNING: &str = "warning";
/// Failure message type (`TypeFailure` in Go).
pub const TYPE_FAILURE: &str = "failure";

/// Message format, a string as in Go (`type MsgFormat string`).
pub type MsgFormat = String;
/// Plain text format (`FormatText` in Go).
pub const FORMAT_TEXT: &str = "text";
/// Markdown format (`FormatMarkdown` in Go).
pub const FORMAT_MARKDOWN: &str = "markdown";
/// HTML format (`FormatHTML` in Go).
pub const FORMAT_HTML: &str = "html";

/// Message sent to the Apprise API.
///
/// Field names and `omitempty` semantics mirror the Go `Message` struct so the
/// serialized JSON is wire-compatible.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Message {
    /// Optional title to go along with the body.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub title: String,
    /// Main message content. This is the only required field.
    pub body: String,
    /// Message type: `info`, `success`, `warning` or `failure`.
    #[serde(rename = "type", default, skip_serializing_if = "String::is_empty")]
    pub r#type: MsgType,
    /// Tag used to notify only tagged endpoints. Comma ORs, space ANDs.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub tag: String,
    /// Text format: `text`, `markdown` or `html`.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub format: MsgFormat,
}

/// Response data required to interpret an Apprise reply.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HttpResponse {
    /// Numeric HTTP status code (for the success check).
    pub status_code: u16,
    /// Go-style status text, e.g. `404 Not Found`, used in error messages.
    pub status: String,
    /// Raw response body bytes.
    pub body: Vec<u8>,
}

/// Errors reported by an injected HTTP client.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HttpClientError {
    /// The request could not be built (e.g. an invalid URL).
    CreateRequest(String),
    /// The request could not be sent.
    Send(String),
}

/// Result of an injected HTTP client request.
pub type HttpResult<T> = std::result::Result<T, HttpClientError>;

/// Minimal HTTP client abstraction used by [`Notifier`].
///
/// Implement this trait to inject a fake in tests; no real network access is
/// then required. The production implementation is [`ReqwestClient`].
#[async_trait::async_trait]
pub trait HttpClient: Send + Sync {
    /// POST `payload` as JSON to `url` and return the raw response.
    async fn post(&self, url: &str, payload: &[u8]) -> HttpResult<HttpResponse>;
}

/// Built-in [`HttpClient`] backed by `reqwest` (rustls, no system proxy).
pub struct ReqwestClient {
    client: std::result::Result<reqwest::Client, String>,
}

impl Default for ReqwestClient {
    fn default() -> Self {
        let client = reqwest::Client::builder()
            .timeout(DEFAULT_TIMEOUT)
            .connect_timeout(DEFAULT_CONNECT_TIMEOUT)
            .build()
            .map_err(|error| format!("failed to create HTTP client: {error}"));
        Self { client }
    }
}

#[async_trait::async_trait]
impl HttpClient for ReqwestClient {
    async fn post(&self, url: &str, payload: &[u8]) -> HttpResult<HttpResponse> {
        let client = self.client.as_ref().map_err(|error| {
            // A failure to build the client is surfaced as a request error;
            // this is exceptionally rare (TLS backend initialization).
            HttpClientError::CreateRequest(error.clone())
        })?;

        let request = client
            .post(url)
            .header(reqwest::header::CONTENT_TYPE, "application/json")
            .body(payload.to_vec())
            .build()
            .map_err(|error| HttpClientError::CreateRequest(error.to_string()))?;

        let response = client
            .execute(request)
            .await
            .map_err(|error| HttpClientError::Send(error.to_string()))?;

        let status_code = response.status().as_u16();
        let reason = response.status().canonical_reason().unwrap_or("");
        let status = if reason.is_empty() {
            status_code.to_string()
        } else {
            format!("{status_code} {reason}")
        };

        // Go ignores a body read failure (io.ReadAll) and falls back to a
        // status-only error, so do the same instead of failing the request.
        let body = response.bytes().await.map_or_else(|_| Vec::new(), |b| b.to_vec());

        Ok(HttpResponse {
            status_code,
            status,
            body,
        })
    }
}

/// Errors returned by [`Notifier::send`].
///
/// Display strings match the Go `fmt.Errorf` messages where observable.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum Error {
    #[error("the message must have a body")]
    EmptyBody,
    #[error("the message must have a tag")]
    Untagged,
    #[error("failed to marshal message: {0}")]
    Marshal(String),
    #[error("failed to create request: {0}")]
    CreateRequest(String),
    #[error("failed to send message: {0}")]
    Send(String),
    #[error("failed to send message: apprise returned {status:?} with an error message: {message}")]
    AppriseError { status: String, message: String },
    #[error("failed to send message: {status} (body teaser: {teaser})")]
    BodyTeaser { status: String, teaser: String },
    #[error("failed to send message: {status}")]
    Status { status: String },
}

/// Convenience result alias for [`Notifier::send`].
pub type Result<T> = std::result::Result<T, Error>;

/// Sends messages to an Apprise API.
///
/// Mirrors the Go `Notifier`: public configuration fields, an optionally
/// injected HTTP client, and the same validation/defaulting/send semantics.
pub struct Notifier {
    /// Apprise API endpoint.
    pub url: String,
    /// Default message type, applied when a message has none.
    pub default_type: MsgType,
    /// Default message tag, applied when a message has none.
    pub default_tag: String,
    /// Default message format, applied when a message has none.
    pub default_format: MsgFormat,
    /// Allow untagged messages (sent to all configured endpoints).
    pub allow_untagged: bool,
    client: RwLock<Option<Arc<dyn HttpClient>>>,
}

impl Default for Notifier {
    fn default() -> Self {
        Self {
            url: String::new(),
            default_type: String::new(),
            default_tag: String::new(),
            default_format: String::new(),
            allow_untagged: false,
            client: RwLock::new(None),
        }
    }
}

impl Notifier {
    /// Creates a notifier for the given endpoint with no defaults set.
    pub fn new(url: impl Into<String>) -> Self {
        Self {
            url: url.into(),
            ..Self::default()
        }
    }

    /// Sets a custom HTTP client, like Go's `Notifier.SetClient`.
    ///
    /// Useful for injecting a fake in tests or a client with a client
    /// certificate in production.
    pub fn set_client(&self, client: Arc<dyn HttpClient>) {
        *self.client.write().expect("notifier client lock poisoned") = Some(client);
    }

    /// Returns the injected client, lazily creating the default one if needed.
    ///
    /// The lock is released before any await point.
    fn client(&self) -> Arc<dyn HttpClient> {
        {
            let guard = self.client.read().expect("notifier client lock poisoned");
            if let Some(client) = guard.as_ref() {
                return Arc::clone(client);
            }
        }

        let mut guard = self.client.write().expect("notifier client lock poisoned");
        Arc::clone(
            guard
                .get_or_insert_with(|| Arc::new(ReqwestClient::default()))
        )
    }

    /// Applies the notifier defaults to a message, like Go's `applyDefaults`.
    fn apply_defaults(&self, message: &mut Message) {
        if message.r#type.is_empty() {
            message.r#type = self.default_type.clone();
        }
        if message.tag.is_empty() {
            message.tag = self.default_tag.clone();
        }
        if message.format.is_empty() {
            message.format = self.default_format.clone();
        }
    }

    /// Sends a message to the Apprise API.
    ///
    /// Like the Go version, defaults are applied in place on `message` before
    /// the payload is built, and a message without a body or (unless allowed)
    /// a tag is rejected before any request is made.
    pub async fn send(&self, message: &mut Message) -> Result<()> {
        // Check if the message has a body.
        if message.body.is_empty() {
            return Err(Error::EmptyBody);
        }

        // Apply notifier defaults.
        self.apply_defaults(message);

        // Check if the message is tagged.
        if message.tag.is_empty() && !self.allow_untagged {
            return Err(Error::Untagged);
        }

        // Marshal the message to JSON.
        let payload = to_vec(message).map_err(|error| Error::Marshal(error.to_string()))?;

        // Send the message to the API.
        let response = self
            .client()
            .post(&self.url, &payload)
            .await
            .map_err(|error| match error {
                HttpClientError::CreateRequest(error) => Error::CreateRequest(error),
                HttpClientError::Send(error) => Error::Send(error),
            })?;

        match response.status_code {
            // http.StatusOK, StatusCreated, StatusNoContent, StatusAccepted.
            200 | 201 | 204 | 202 => Ok(()),
            _ => {
                // Try to tease body contents.
                if !response.body.is_empty() {
                    // Try to parse a JSON error response.
                    if let Ok(error_response) = serde_json::from_slice::<ErrorResponse>(&response.body) {
                        if !error_response.error.is_empty() {
                            return Err(Error::AppriseError {
                                status: response.status.clone(),
                                message: error_response.error,
                            });
                        }
                    }
                    return Err(Error::BodyTeaser {
                        status: response.status.clone(),
                        teaser: safe_first_16_bytes(&response.body),
                    });
                }
                Err(Error::Status {
                    status: response.status,
                })
            }
        }
    }
}

/// JSON error body returned by Apprise: `{"error": "..."}`.
#[derive(Debug, Deserialize)]
struct ErrorResponse {
    #[serde(default)]
    error: String,
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use super::{
        Error, HttpClient, HttpClientError, HttpResult, HttpResponse, Message, Notifier,
        FORMAT_HTML, FORMAT_MARKDOWN, TYPE_INFO, TYPE_WARNING,
    };

    #[derive(Debug, Clone)]
    struct RecordedRequest {
        url: String,
        payload: Vec<u8>,
    }

    struct FakeClient {
        response: HttpResult<HttpResponse>,
        requests: Arc<Mutex<Vec<RecordedRequest>>>,
    }

    impl FakeClient {
        fn new(response: HttpResult<HttpResponse>) -> (Self, Arc<Mutex<Vec<RecordedRequest>>>) {
            let requests = Arc::new(Mutex::new(Vec::new()));
            (
                Self {
                    response,
                    requests: Arc::clone(&requests),
                },
                requests,
            )
        }
    }

    #[async_trait::async_trait]
    impl HttpClient for FakeClient {
        async fn post(&self, url: &str, payload: &[u8]) -> HttpResult<HttpResponse> {
            self.requests.lock().unwrap().push(RecordedRequest {
                url: url.to_string(),
                payload: payload.to_vec(),
            });
            self.response.clone()
        }
    }

    fn ok(status_code: u16, status: &str) -> HttpResult<HttpResponse> {
        Ok(HttpResponse {
            status_code,
            status: status.to_string(),
            body: Vec::new(),
        })
    }

    fn with_status(status_code: u16, status: &str, body: &[u8]) -> HttpResult<HttpResponse> {
        Ok(HttpResponse {
            status_code,
            status: status.to_string(),
            body: body.to_vec(),
        })
    }

    fn notifier(response: HttpResult<HttpResponse>) -> (Notifier, Arc<Mutex<Vec<RecordedRequest>>>) {
        let (client, requests) = FakeClient::new(response);
        let notifier = Notifier {
            url: "https://apprise.example/notify".to_string(),
            default_type: TYPE_INFO.to_string(),
            default_tag: "service".to_string(),
            default_format: FORMAT_MARKDOWN.to_string(),
            allow_untagged: false,
            ..Notifier::default()
        };
        notifier.set_client(Arc::new(client));
        (notifier, requests)
    }

    async fn send(msg: Message) -> Result<(), Error> {
        let (notifier, _) = notifier(ok(200, "200 OK"));
        let mut msg = msg;
        notifier.send(&mut msg).await
    }

    #[tokio::test]
    async fn rejects_empty_body() {
        let mut message = Message {
            title: "title".to_string(),
            body: String::new(),
            tag: "service".to_string(),
            ..Message::default()
        };
        assert_eq!(send(message.clone()).await, Err(Error::EmptyBody));
        message.body = "hello".to_string();
        assert!(send(message).await.is_ok());
    }

    #[tokio::test]
    async fn rejects_untagged_message() {
        let (client, _) = FakeClient::new(ok(200, "200 OK"));
        let notifier = Notifier {
            url: "https://apprise.example/notify".to_string(),
            // No default tag and untagged messages are not allowed.
            ..Notifier::default()
        };
        notifier.set_client(Arc::new(client));
        let mut message = Message {
            body: "hello".to_string(),
            ..Message::default()
        };
        assert_eq!(notifier.send(&mut message).await, Err(Error::Untagged));
    }

    #[tokio::test]
    async fn allows_untagged_message_when_configured() {
        let (client, _) = FakeClient::new(ok(200, "200 OK"));
        let notifier = Notifier {
            url: "https://apprise.example/notify".to_string(),
            allow_untagged: true,
            ..Notifier::default()
        };
        notifier.set_client(Arc::new(client));
        let mut message = Message {
            body: "hello".to_string(),
            ..Message::default()
        };
        assert!(notifier.send(&mut message).await.is_ok());
    }

    #[tokio::test]
    async fn payload_shape_and_defaults() {
        let (notifier, requests) = notifier(ok(200, "200 OK"));
        let mut message = Message {
            title: "A title".to_string(),
            body: "A body".to_string(),
            ..Message::default()
        };
        notifier.send(&mut message).await.unwrap();

        // Defaults were applied in place, like Go's applyDefaults.
        assert_eq!(message.r#type, TYPE_INFO);
        assert_eq!(message.tag, "service");
        assert_eq!(message.format, FORMAT_MARKDOWN);

        let requests = requests.lock().unwrap();
        assert_eq!(requests.len(), 1);
        assert_eq!(requests[0].url, "https://apprise.example/notify");
        let payload: serde_json::Value = serde_json::from_slice(&requests[0].payload).unwrap();
        assert_eq!(
            payload,
            serde_json::json!({
                "title": "A title",
                "body": "A body",
                "type": "info",
                "tag": "service",
                "format": "markdown",
            })
        );
    }

    #[tokio::test]
    async fn payload_omits_empty_optional_fields() {
        let (client, requests) = FakeClient::new(ok(200, "200 OK"));
        let notifier = Notifier {
            url: "https://apprise.example/notify".to_string(),
            allow_untagged: true,
            ..Notifier::default()
        };
        notifier.set_client(Arc::new(client));
        let mut message = Message {
            body: "only body".to_string(),
            ..Message::default()
        };
        notifier.send(&mut message).await.unwrap();

        let requests = requests.lock().unwrap();
        let payload: serde_json::Value = serde_json::from_slice(&requests[0].payload).unwrap();
        assert_eq!(payload, serde_json::json!({ "body": "only body" }));
    }

    #[tokio::test]
    async fn explicit_fields_override_defaults() {
        let (notifier, requests) = notifier(ok(200, "200 OK"));
        let mut message = Message {
            body: "body".to_string(),
            r#type: TYPE_WARNING.to_string(),
            format: FORMAT_HTML.to_string(),
            ..Message::default()
        };
        notifier.send(&mut message).await.unwrap();

        assert_eq!(message.r#type, TYPE_WARNING);
        assert_eq!(message.format, FORMAT_HTML);
        let requests = requests.lock().unwrap();
        let payload: serde_json::Value = serde_json::from_slice(&requests[0].payload).unwrap();
        assert_eq!(payload["type"], TYPE_WARNING);
        assert_eq!(payload["format"], FORMAT_HTML);
    }

    #[tokio::test]
    async fn success_statuses_are_accepted() {
        for (code, text) in [(200, "200 OK"), (201, "201 Created"), (202, "202 Accepted"), (204, "204 No Content")] {
            let (notifier, _) = notifier(ok(code, text));
            let mut message = Message {
                body: "body".to_string(),
                ..Message::default()
            };
            assert!(notifier.send(&mut message).await.is_ok(), "status {code}");
        }
    }

    #[tokio::test]
    async fn json_error_response() {
        let (notifier, _) = notifier(with_status(
            400,
            "400 Bad Request",
            br#"{"error":"invalid tag"}"#,
        ));
        let mut message = Message {
            body: "body".to_string(),
            ..Message::default()
        };
        assert_eq!(
            notifier.send(&mut message).await,
            Err(Error::AppriseError {
                status: "400 Bad Request".to_string(),
                message: "invalid tag".to_string(),
            })
        );
    }

    #[tokio::test]
    async fn non_json_error_uses_body_teaser() {
        let (notifier, _) = notifier(with_status(500, "500 Internal Server Error", b"boom!"));
        let mut message = Message {
            body: "body".to_string(),
            ..Message::default()
        };
        assert_eq!(
            notifier.send(&mut message).await,
            Err(Error::BodyTeaser {
                status: "500 Internal Server Error".to_string(),
                teaser: "62 6f 6f 6d 21                                    |boom!|".to_string(),
            })
        );
    }

    #[tokio::test]
    async fn empty_error_body_uses_status_only() {
        let (notifier, _) = notifier(ok(503, "503 Service Unavailable"));
        let mut message = Message {
            body: "body".to_string(),
            ..Message::default()
        };
        assert_eq!(
            notifier.send(&mut message).await,
            Err(Error::Status {
                status: "503 Service Unavailable".to_string(),
            })
        );
    }

    #[tokio::test]
    async fn object_without_error_field_uses_teaser() {
        let (notifier, _) = notifier(with_status(418, "418 I'm a teapot", br#"{"other":"value"}"#));
        let mut message = Message {
            body: "body".to_string(),
            ..Message::default()
        };
        let error = notifier.send(&mut message).await.unwrap_err();
        assert!(matches!(error, Error::BodyTeaser { .. }), "{error:?}");
    }

    #[tokio::test]
    async fn transport_errors_are_wrapped() {
        let (notifier, _) = notifier(Err(HttpClientError::Send("connection refused".to_string())));
        let mut message = Message {
            body: "body".to_string(),
            ..Message::default()
        };
        assert_eq!(
            notifier.send(&mut message).await,
            Err(Error::Send("connection refused".to_string()))
        );
        assert_eq!(
            Error::Send("connection refused".to_string()).to_string(),
            "failed to send message: connection refused"
        );
    }

    #[tokio::test]
    async fn request_creation_errors_are_wrapped() {
        let (notifier, _) = notifier(Err(HttpClientError::CreateRequest("bad url".to_string())));
        let mut message = Message {
            body: "body".to_string(),
            ..Message::default()
        };
        assert_eq!(
            notifier.send(&mut message).await,
            Err(Error::CreateRequest("bad url".to_string()))
        );
    }
}
