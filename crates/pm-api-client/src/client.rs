//! WebSocket client for the Portmaster database API.
//!
//! Mirrors the Go `base/api/client` package: a single connection multiplexes
//! operations identified by `opID`, replies are dispatched to per-operation
//! channels, and subscriptions can be resuscitated after a reconnect.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use serde_json::Value;
use tokio::sync::{mpsc, watch};
use tokio::task::JoinHandle;
use tokio_tungstenite::tungstenite::client::IntoClientRequest;
use tokio_tungstenite::tungstenite::{http, Message as WsMessage};
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};

use crate::error::{Error, Result};
use crate::message::{msg, Message};

/// Delay between reconnection attempts, like Go `backOffTimer`.
pub const BACKOFF: Duration = Duration::from_secs(1);

/// Path of the database WebSocket endpoint.
pub const DB_API_PATH: &str = "/api/database/v1";

struct OperationState {
    sender: mpsc::UnboundedSender<Message>,
    /// Last request, stored so it can be resent after reconnecting.
    request: Option<Message>,
    resuscitate: bool,
}

struct State {
    next_op_id: AtomicU64,
    operations: Mutex<HashMap<String, OperationState>>,
    outgoing_tx: mpsc::UnboundedSender<Message>,
    /// The receiver is taken by the writer of the active connection and given
    /// back when that connection ends, so pending requests survive reconnects.
    outgoing_rx: Mutex<Option<mpsc::UnboundedReceiver<Message>>>,
    /// `true` while the WebSocket session is live.
    online: watch::Sender<bool>,
    shutdown: CancellationToken,
    last_error: Mutex<String>,
}

impl State {
    fn dispatch(&self, data: &[u8]) {
        match Message::parse(data) {
            Ok(message) => {
                let operations = self.operations.lock().expect("lock poisoned");
                match operations.get(&message.op_id) {
                    Some(operation) => {
                        debug!(
                            "client: [{}] received {} msg: {}",
                            message.op_id, message.msg_type, message.key
                        );
                        if operation.sender.send(message).is_err() {
                            debug!("client: receiver of operation dropped");
                        }
                    }
                    None => debug!(
                        "client: received message for unknown operation {}",
                        message.op_id
                    ),
                }
            }
            Err(err) => warn!("client: failed to parse message: {err}"),
        }
    }

    fn resuscitate(&self) {
        let operations = self.operations.lock().expect("lock poisoned");
        for operation in operations.values() {
            if operation.resuscitate {
                if let Some(request) = &operation.request {
                    let _ = self.outgoing_tx.send(request.clone());
                    debug!(
                        "client: resuscitated {} {} {}",
                        request.op_id, request.msg_type, request.key
                    );
                }
            }
        }
    }

    fn notify_offline(&self) {
        let operations = self.operations.lock().expect("lock poisoned");
        for (id, operation) in operations.iter() {
            let _ = operation.sender.send(Message::offline(id.clone()));
        }
    }

    fn log_error(&self, err: &Error) {
        let message = err.to_string();
        let mut last = self.last_error.lock().expect("lock poisoned");
        if *last != message {
            warn!("client: error connecting to Portmaster: {message}");
            *last = message;
        }
    }
}

/// A client for the Portmaster database API.
///
/// The client is cheap to clone; all clones share one connection and one set
/// of operations. Either drive the connection with [`Client::connect`] or spawn
/// [`Client::stay_connected`] to keep it online with a one-second backoff.
#[derive(Clone)]
pub struct Client {
    server: String,
    token: Option<String>,
    session_cookie: Option<String>,
    state: Arc<State>,
}

impl Client {
    /// Creates a client for `server`, given as `host:port`.
    pub fn new(server: impl Into<String>) -> Client {
        let (outgoing_tx, outgoing_rx) = mpsc::unbounded_channel();
        let (online, _) = watch::channel(false);
        Client {
            server: server.into(),
            token: None,
            session_cookie: None,
            state: Arc::new(State {
                next_op_id: AtomicU64::new(0),
                operations: Mutex::new(HashMap::new()),
                outgoing_tx,
                outgoing_rx: Mutex::new(Some(outgoing_rx)),
                online,
                shutdown: CancellationToken::new(),
                last_error: Mutex::new(String::new()),
            }),
        }
    }

    /// Authenticates with an HTTP bearer token (`Portmaster-API-Token` config key).
    #[must_use]
    pub fn with_token(mut self, token: impl Into<String>) -> Client {
        self.token = Some(token.into());
        self
    }

    /// Authenticates with a session cookie value.
    #[must_use]
    pub fn with_session_cookie(mut self, value: impl Into<String>) -> Client {
        self.session_cookie = Some(value.into());
        self
    }

    /// Connects once and runs the WebSocket session until it ends.
    ///
    /// Requests created before or during the session are written as soon as the
    /// connection is up. When the session ends, every operation receives an
    /// [`msg::OFFLINE`] message.
    pub async fn connect(&self) -> Result<()> {
        match self.connect_inner().await {
            Ok(()) => Ok(()),
            Err(err) => {
                self.state.log_error(&err);
                Err(err)
            }
        }
    }

    async fn connect_inner(&self) -> Result<()> {
        let request = self.build_request()?;
        let (websocket, _response) = tokio_tungstenite::connect_async(request)
            .await
            .map_err(|err| Error::WebSocket(err.to_string()))?;

        let receiver = self
            .state
            .outgoing_rx
            .lock()
            .expect("lock poisoned")
            .take()
            .ok_or(Error::AlreadyConnected)?;

        // Going online resends all operations that opted into resuscitation.
        self.state.online.send_replace(true);
        self.state.resuscitate();

        let (mut write, mut read) = websocket.split();
        let fail = CancellationToken::new();

        let writer_fail = fail.clone();
        let writer_state = Arc::clone(&self.state);
        let writer: JoinHandle<()> = tokio::spawn(async move {
            let mut receiver = receiver;
            loop {
                tokio::select! {
                    _ = writer_fail.cancelled() => break,
                    incoming = receiver.recv() => {
                        let Some(message) = incoming else { break };
                        match message.pack() {
                            Ok(data) => {
                                debug!("client: sent message: {}", String::from_utf8_lossy(&data));
                                if let Err(err) = write.send(WsMessage::Binary(data.into())).await {
                                    warn!("client: write error: {err}");
                                    // Keep the unsent request for the next connection.
                                    let _ = writer_state.outgoing_tx.send(message);
                                    writer_fail.cancel();
                                    break;
                                }
                            }
                            Err(err) => warn!("client: failed to pack message: {err}"),
                        }
                    }
                }
            }
            // Hand the request queue to the next connection.
            *writer_state.outgoing_rx.lock().expect("lock poisoned") = Some(receiver);
        });

        loop {
            tokio::select! {
                incoming = read.next() => {
                    match incoming {
                        Some(Ok(WsMessage::Binary(data))) => self.state.dispatch(&data),
                        Some(Ok(WsMessage::Text(text))) => self.state.dispatch(text.as_bytes()),
                        Some(Ok(WsMessage::Ping(_)))
                        | Some(Ok(WsMessage::Pong(_)))
                        | Some(Ok(WsMessage::Frame(_))) => {}
                        Some(Ok(WsMessage::Close(_))) => {
                            debug!("client: connection closed by server");
                            break;
                        }
                        Some(Err(err)) => {
                            warn!("client: read error: {err}");
                            break;
                        }
                        None => {
                            debug!("client: connection closed");
                            break;
                        }
                    }
                }
                _ = fail.cancelled() => break,
                _ = self.state.shutdown.cancelled() => break,
            }
        }

        fail.cancel();
        self.state.online.send_replace(false);
        self.state.notify_offline();
        let _ = writer.await;
        Ok(())
    }

    fn build_request(&self) -> Result<http::Request<()>> {
        let mut request = format!("ws://{}{}", self.server, DB_API_PATH)
            .into_client_request()
            .map_err(|err| Error::WebSocket(err.to_string()))?;

        if let Some(token) = &self.token {
            let value = format!("Bearer {token}");
            let header = http::HeaderValue::from_str(&value)
                .map_err(|err| Error::InvalidHeader(err.to_string()))?;
            request.headers_mut().insert(http::header::AUTHORIZATION, header);
        }
        if let Some(cookie) = &self.session_cookie {
            let value = format!("Portmaster-API-Token={cookie}");
            let header = http::HeaderValue::from_str(&value)
                .map_err(|err| Error::InvalidHeader(err.to_string()))?;
            request.headers_mut().insert(http::header::COOKIE, header);
        }

        Ok(request)
    }

    /// Spawns a background task that reconnects with [`BACKOFF`] after every
    /// disconnect or failed attempt, until [`Client::shutdown`] is called.
    pub fn stay_connected(self: &Arc<Self>) -> JoinHandle<()> {
        let client = Arc::clone(self);
        tokio::spawn(async move {
            debug!("client: connecting to Portmaster at {}", client.server);
            loop {
                if client.state.shutdown.is_cancelled() {
                    break;
                }
                let _ = client.connect().await;
                tokio::select! {
                    _ = tokio::time::sleep(BACKOFF) => {}
                    _ = client.state.shutdown.cancelled() => break,
                }
            }
            debug!("client: stay-connected loop stopped");
        })
    }

    /// Stops the client and any [`Client::stay_connected`] task.
    pub fn shutdown(&self) {
        self.state.shutdown.cancel();
    }

    /// Returns a watch receiver that yields `true` while connected.
    pub fn online(&self) -> watch::Receiver<bool> {
        self.state.online.subscribe()
    }

    /// Returns whether the client is currently connected.
    pub fn is_online(&self) -> bool {
        *self.state.online.borrow()
    }

    fn new_operation(&self) -> Operation {
        let id = format!("#{}", self.state.next_op_id.fetch_add(1, Ordering::Relaxed) + 1);
        let (sender, receiver) = mpsc::unbounded_channel();
        self.state.operations.lock().expect("lock poisoned").insert(
            id.clone(),
            OperationState {
                sender,
                request: None,
                resuscitate: false,
            },
        );
        Operation {
            id,
            receiver,
            client: self.clone(),
        }
    }

    /// Sends a `get` request for a single record.
    pub fn get(&self, key: &str) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::GET, key, None);
        operation
    }

    /// Sends a `query` request, replying with `ok` per record and then `done`.
    pub fn query(&self, query: &str) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::QUERY, query, None);
        operation
    }

    /// Subscribes to changes matching a query.
    pub fn sub(&self, query: &str) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::SUB, query, None);
        operation.enable_resuscitation();
        operation
    }

    /// Runs a query and then subscribes to changes matching it.
    pub fn qsub(&self, query: &str) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::QSUB, query, None);
        operation.enable_resuscitation();
        operation
    }

    /// Creates a record with the given JSON value.
    pub fn create(&self, key: &str, value: Value) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::CREATE, key, Some(value));
        operation
    }

    /// Replaces a record with the given JSON value.
    pub fn update(&self, key: &str, value: Value) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::UPDATE, key, Some(value));
        operation
    }

    /// Merges attributes into an existing record.
    pub fn insert(&self, key: &str, value: Value) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::INSERT, key, Some(value));
        operation
    }

    /// Deletes a record.
    pub fn delete(&self, key: &str) -> Operation {
        let operation = self.new_operation();
        operation.send(msg::DELETE, key, None);
        operation
    }
}

/// A single request and its stream of replies.
pub struct Operation {
    id: String,
    receiver: mpsc::UnboundedReceiver<Message>,
    client: Client,
}

impl Operation {
    /// Returns this operation's id.
    pub fn id(&self) -> &str {
        &self.id
    }

    /// Sends a raw request. Used by the [`Client`] helpers and available for
    /// operations not covered by them.
    pub fn send(&self, msg_type: &str, key: &str, value: Option<Value>) {
        let request = Message::request(self.id.clone(), msg_type, key, value);
        if let Some(operation) = self
            .client
            .state
            .operations
            .lock()
            .expect("lock poisoned")
            .get_mut(&self.id)
        {
            operation.request = Some(request.clone());
        }
        let _ = self.client.state.outgoing_tx.send(request);
    }

    /// Resends this operation's request after a reconnect.
    pub fn enable_resuscitation(&self) {
        if let Some(operation) = self
            .client
            .state
            .operations
            .lock()
            .expect("lock poisoned")
            .get_mut(&self.id)
        {
            operation.resuscitate = true;
        }
    }

    /// Waits for the next reply message.
    pub async fn recv(&mut self) -> Option<Message> {
        self.receiver.recv().await
    }

    /// Cancels the operation; it will no longer receive messages.
    pub fn cancel(&self) {
        self.client
            .state
            .operations
            .lock()
            .expect("lock poisoned")
            .remove(&self.id);
    }
}

impl Drop for Operation {
    fn drop(&mut self) {
        self.client
            .state
            .operations
            .lock()
            .expect("lock poisoned")
            .remove(&self.id);
    }
}
