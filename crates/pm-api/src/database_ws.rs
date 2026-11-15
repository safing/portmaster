//! Database WebSocket API, served at `/api/database/v1`.
//!
//! Frames are `opID|type|key|data`, split like Go's `bytes.SplitN(msg, "|", 3)`.
//! Record data is `J` (the JSON format byte) followed by the JSON payload with a
//! `_meta` object that includes the record key.
//!
//! Supported operations: `get`, `query`, `sub`, `qsub`, `create`, `update`,
//! `insert`, `delete` and `cancel`. See [`DatabaseApi::handle`] for the exact
//! replies.
//!
//! # Compatibility notes
//!
//! * Deleted records are reported as `del` frames before marshalling. The Go
//!   implementation tries to marshal deleted records first, fails, and sends a
//!   `warning` instead; the documented protocol and the UI expect `del`.
//! * Queries are executed synchronously (the Rust storage returns a vector), so
//!   `cancel` only affects subscriptions; a cancelled query has already sent
//!   `done`.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::extract::{FromRequestParts, State};
use axum::response::IntoResponse;
use pm_database::{Databases, Interface, Key, Meta, Options, Query, Record};
use serde_json::Value;
use tokio::sync::mpsc::{self, UnboundedReceiver, UnboundedSender};
use tokio_util::sync::CancellationToken;

use crate::{Api, Response, PERMIT_ADMIN};

pub const DB_API_PATH: &str = "/api/database/v1";

const SEPARATOR: u8 = b'|';

/// A database API connection.
pub struct DatabaseApi {
    db: Arc<Interface>,
    sender: UnboundedSender<Vec<u8>>,
    shutdown: CancellationToken,
    subscriptions: Mutex<HashMap<String, CancellationToken>>,
}

impl DatabaseApi {
    /// Creates an API around the given databases and returns the frame receiver.
    pub fn new(databases: Arc<Databases>) -> (Arc<DatabaseApi>, UnboundedReceiver<Vec<u8>>) {
        let (sender, receiver) = mpsc::unbounded_channel();
        let api = Arc::new(DatabaseApi {
            db: Arc::new(databases.interface(Options::default())),
            sender,
            shutdown: CancellationToken::new(),
            subscriptions: Mutex::new(HashMap::new()),
        });
        (api, receiver)
    }

    /// Shuts the API down: cancels all subscriptions and stops delivery.
    pub fn shutdown(&self) {
        self.shutdown.cancel();
    }

    /// Handles one frame from the client.
    ///
    /// ``opID|get|<key>``
    ///     ``opID|ok|<key>|<data>`` / ``opID|error|<message>``
    ///
    /// ``opID|query|<query>``
    ///     ``ok`` per record, then ``done``, or ``error``
    ///
    /// ``opID|sub|<query>``
    ///     ``upd``/``new``/``del`` frames, ``warning`` for bad records
    ///
    /// ``opID|qsub|<query>``
    ///     ``ok`` per record, ``done``, then subscription updates
    ///
    /// ``opID|cancel``
    ///     cancels a subscription, which replies with ``done``
    ///
    /// ``opID|create|<key>|<data>``/``update``/``insert``/``delete``
    ///     ``success`` or ``error``
    pub fn handle(self: &Arc<Self>, message: &[u8]) {
        let parts: Vec<&[u8]> = message.splitn(3, |&byte| byte == SEPARATOR).collect();

        if parts.len() == 2 && parts[1] == b"cancel" {
            self.handle_cancel(parts[0]);
            return;
        }
        if parts.len() != 3 {
            self.send(&[], "error", Some("bad request: malformed message"), None);
            return;
        }

        let op = parts[0];
        match parts[1] {
            b"get" => self.handle_get(op, parts[2]),
            b"query" => self.handle_query(op, parts[2]),
            b"sub" => self.handle_sub(op, parts[2]),
            b"qsub" => self.handle_qsub(op, parts[2]),
            b"create" | b"update" | b"insert" => {
                let mut data_parts = parts[2].splitn(2, |&byte| byte == SEPARATOR);
                let (Some(key), Some(data)) = (data_parts.next(), data_parts.next()) else {
                    self.send(&[], "error", Some("bad request: malformed message"), None);
                    return;
                };
                match parts[1] {
                    b"create" => self.handle_put(op, key, data, true),
                    b"update" => self.handle_put(op, key, data, false),
                    _ => self.handle_insert(op, key, data),
                }
            }
            b"delete" => self.handle_delete(op, parts[2]),
            _ => self.send(op, "error", Some("bad request: unknown method"), None),
        }
    }

    fn send(&self, op: &[u8], message_type: &str, key: Option<&str>, data: Option<&[u8]>) {
        let _ = self.sender.send(build_frame(op, message_type, key, data));
    }

    fn handle_get(&self, op: &[u8], key: &[u8]) {
        let key = scheme(key);
        let record = match self.db.get(&key) {
            Ok(record) => record,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        };
        match record.to_json_with_meta(true) {
            Ok(data) => self.send(op, "ok", Some(&record.key.to_string()), Some(&data)),
            Err(err) => self.send(op, "error", Some(&err.to_string()), None),
        }
    }

    fn handle_query(&self, op: &[u8], query_text: &[u8]) {
        let query = match pm_query::parse_query(&scheme(query_text)) {
            Ok(query) => query,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        };
        self.process_query(op, &query);
    }

    fn process_query(&self, op: &[u8], query: &Query) {
        let records = match self.db.query(query) {
            Ok(records) => records,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        };

        for record in records {
            let key = record.key.to_string();
            match record.to_json_with_meta(true) {
                Ok(data) => self.send(op, "ok", Some(&key), Some(&data)),
                Err(err) => self.send(op, "warning", Some(&err.to_string()), None),
            }
        }
        self.send(op, "done", None, None);
    }

    fn handle_sub(self: &Arc<Self>, op: &[u8], query_text: &[u8]) {
        let query = match pm_query::parse_query(&scheme(query_text)) {
            Ok(query) => query,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        };

        if let Some(subscription) = self.subscribe(op, &query) {
            self.spawn_subscription_reader(op, subscription);
        }
    }

    fn handle_qsub(self: &Arc<Self>, op: &[u8], query_text: &[u8]) {
        let query = match pm_query::parse_query(&scheme(query_text)) {
            Ok(query) => query,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        };

        // Subscribe before querying, so that no update can be missed.
        let Some(subscription) = self.subscribe(op, &query) else {
            return;
        };
        self.process_query(op, &query);
        self.spawn_subscription_reader(op, subscription);
    }

    fn subscribe(&self, op: &[u8], query: &Query) -> Option<pm_database::Subscription> {
        match self.db.subscribe(query.clone()) {
            Ok(subscription) => Some(subscription),
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                None
            }
        }
    }

    fn spawn_subscription_reader(
        self: &Arc<Self>,
        op: &[u8],
        mut subscription: pm_database::Subscription,
    ) {
        let token = self.shutdown.child_token();
        let op_string = scheme(op);
        self.subscriptions
            .lock()
            .expect("lock poisoned")
            .insert(op_string.clone(), token.clone());

        let api = Arc::clone(self);
        let op = op.to_vec();
        tokio::spawn(async move {
            loop {
                tokio::select! {
                    _ = token.cancelled() => {
                        subscription.cancel();
                        api.send(&op, "done", None, None);
                        break;
                    }
                    record = subscription.recv() => {
                        let Some(record) = record else {
                            api.send(&op, "done", None, None);
                            break;
                        };
                        api.send_subscription_record(&op, &record);
                    }
                }
            }
            api.subscriptions
                .lock()
                .expect("lock poisoned")
                .remove(&op_string);
        });
    }

    fn send_subscription_record(&self, op: &[u8], record: &Record) {
        let key = record.key.to_string();
        if record.meta.is_deleted() {
            self.send(op, "del", Some(&key), None);
            return;
        }
        match record.to_json_with_meta(true) {
            Ok(data) => {
                let message_type = if record.meta.created == record.meta.modified {
                    "new"
                } else {
                    "upd"
                };
                self.send(op, message_type, Some(&key), Some(&data));
            }
            Err(err) => self.send(op, "warning", Some(&err.to_string()), None),
        }
    }

    fn handle_cancel(&self, op: &[u8]) {
        let token = self
            .subscriptions
            .lock()
            .expect("lock poisoned")
            .remove(&scheme(op));
        match token {
            Some(token) => token.cancel(),
            // Queries finish by themselves; only subscriptions can be missing.
            None => self.send(op, "error", Some("could not find subscription"), None),
        }
    }

    fn handle_put(&self, op: &[u8], key: &[u8], data: &[u8], create: bool) {
        if data.len() < 2 {
            self.send(op, "error", Some("bad request: malformed message"), None);
            return;
        }

        let record = Record {
            key: Key::parse(&scheme(key)),
            meta: Meta::default(),
            format: data[0],
            data: data[1..].to_vec(),
        };
        let result = if create {
            self.db.put_new(record)
        } else {
            self.db.put(record)
        };
        match result {
            Ok(()) => self.send(op, "success", None, None),
            Err(err) => self.send(op, "error", Some(&err.to_string()), None),
        }
    }

    fn handle_insert(&self, op: &[u8], key: &[u8], data: &[u8]) {
        let key = scheme(key);
        let mut record = match self.db.get(&key) {
            Ok(record) => record,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        };

        let value: Value = match serde_json::from_slice(data) {
            Ok(value) => value,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        };
        let values = match value {
            Value::Object(values) => values,
            Value::Array(_) => {
                self.send(op, "error", Some("keys must be strings"), None);
                return;
            }
            _ => {
                self.send(op, "error", Some("could not find any valid values"), None);
                return;
            }
        };
        if values.is_empty() {
            self.send(op, "error", Some("could not find any valid values"), None);
            return;
        }

        let Some(mut accessor) = record.accessor() else {
            self.send(op, "error", Some("invalid data"), None);
            return;
        };
        for (name, value) in values {
            if value.is_null() {
                self.send(op, "error", Some("non-existent value"), None);
                return;
            }
            if let Err(err) = accessor.set(&name, value) {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        }

        match pm_dsd::gojson::to_vec(accessor.value()) {
            Ok(data) => record.data = data,
            Err(err) => {
                self.send(op, "error", Some(&err.to_string()), None);
                return;
            }
        }
        match self.db.put(record) {
            Ok(()) => self.send(op, "success", None, None),
            Err(err) => self.send(op, "error", Some(&err.to_string()), None),
        }
    }

    fn handle_delete(&self, op: &[u8], key: &[u8]) {
        match self.db.delete(&scheme(key)) {
            Ok(()) => self.send(op, "success", None, None),
            Err(err) => self.send(op, "error", Some(&err.to_string()), None),
        }
    }
}

/// Builds one frame.
fn build_frame(op: &[u8], message_type: &str, key: Option<&str>, data: Option<&[u8]>) -> Vec<u8> {
    let mut frame = Vec::with_capacity(64);
    frame.extend_from_slice(op);
    frame.push(SEPARATOR);
    frame.extend_from_slice(message_type.as_bytes());
    if let Some(key) = key {
        if !key.is_empty() {
            frame.push(SEPARATOR);
            frame.extend_from_slice(key.as_bytes());
        }
    }
    if let Some(data) = data {
        if !data.is_empty() {
            frame.push(SEPARATOR);
            frame.extend_from_slice(data);
        }
    }
    frame
}

fn scheme(bytes: &[u8]) -> String {
    String::from_utf8_lossy(bytes).into_owned()
}

/// Upgrades a request to the database WebSocket API.
pub async fn database_ws_upgrade(
    State(api): State<Arc<Api>>,
    request: axum::extract::Request,
) -> axum::response::Response {
    let (mut parts, _body) = request.into_parts();

    // Authenticate before upgrading, like Go's auth wrapper.
    let mut api_request = crate::Request::new(parts.method.as_str(), parts.uri.path());
    api_request.host = parts
        .headers
        .get(http::header::HOST)
        .and_then(|value| value.to_str().ok())
        .unwrap_or("")
        .to_string();
    api_request.header = parts.headers.clone();
    api_request.read_method = true;
    if let Err(response) = api.authorize_required(&mut api_request, PERMIT_ADMIN) {
        return response.into_axum();
    }

    let databases = match api.databases() {
        Some(databases) => databases,
        None => {
            return Response::error(503, "database API is not available").into_axum();
        }
    };

    let websocket = match WebSocketUpgrade::from_request_parts(&mut parts, &()).await {
        Ok(websocket) => websocket,
        Err(rejection) => return rejection.into_response(),
    };

    websocket
        .read_buffer_size(1024)
        .write_buffer_size(65536)
        .on_upgrade(move |socket| database_ws_session(socket, databases))
}

async fn database_ws_session(socket: WebSocket, databases: Arc<Databases>) {
    let mut socket = socket;
    let (api, mut frames) = DatabaseApi::new(databases);

    loop {
        tokio::select! {
            frame = frames.recv() => {
                let Some(frame) = frame else {
                    break;
                };
                if socket.send(Message::Binary(frame.into())).await.is_err() {
                    break;
                }
            }
            message = socket.recv() => {
                match message {
                    Some(Ok(Message::Binary(data))) => api.handle(&data),
                    Some(Ok(Message::Text(text))) => api.handle(text.as_bytes()),
                    Some(Ok(Message::Ping(_) | Message::Pong(_))) => {}
                    Some(Ok(Message::Close(_))) | Some(Err(_)) | None => break,
                }
            }
        }
    }

    api.shutdown();
}
