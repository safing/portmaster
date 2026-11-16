//! Apprise notification client, a port of the Go `base/apprise` package.
//!
//! Provides a [`Message`] model with wire-compatible JSON field names, a
//! [`Notifier`] that applies defaults and POSTs messages to an Apprise API,
//! and safe "body teaser" helpers ported from `base/utils/safe.go`.
//!
//! HTTP access is behind the [`HttpClient`] trait so tests can inject a fake
//! and avoid real network calls; [`ReqwestClient`] is the production client,
//! built with bounded timeouts.
//!
//! # Compatibility notes
//!
//! * `Send` is asynchronous (`reqwest` is async) and takes `&mut Message`,
//!   mirroring Go's pointer argument: defaults are applied in place.
//! * Success codes are `200`, `201`, `202` and `204`, as in Go.
//! * Error message strings match the Go `fmt.Errorf` calls where observable.
//! * A body read failure in the default client is treated as an empty body,
//!   like Go's ignored `io.ReadAll` error, yielding a status-only error.
//! * Go's bare `http.Client{}` has no timeout; the built-in client uses a
//!   10s overall and 5s connect timeout on purpose.

mod notify;
mod teaser;

pub use notify::{
    Error, HttpClient, HttpClientError, HttpResult, HttpResponse, Message, MsgFormat, MsgType,
    Notifier, ReqwestClient, Result, DEFAULT_CONNECT_TIMEOUT, DEFAULT_TIMEOUT, FORMAT_HTML,
    FORMAT_MARKDOWN, FORMAT_TEXT, TYPE_FAILURE, TYPE_INFO, TYPE_SUCCESS, TYPE_WARNING,
};
pub use teaser::{safe_first_16_bytes, safe_first_16_chars};
