//! Injectable client interface for the IVPN daemon and its TCP implementation.
//!
//! The Go code talks to the daemon with
//! `github.com/ivpn/desktop-app/daemon/protocol/ivpnclient`. The wire format is
//! simple newline-delimited JSON on `127.0.0.1:<port>` (see the Go module's
//! `connection.go`), so it is reproduced here. Everything that touches the
//! network or the filesystem sits behind [`IvpnClient`] / [`IvpnClientFactory`]
//! and can be replaced by a mock in tests.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use async_trait::async_trait;
use serde_json::{json, Map, Value};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::tcp::OwnedWriteHalf;
use tokio::net::TcpStream;
use tokio::sync::{broadcast, oneshot, Mutex as AsyncMutex};
use tokio_util::sync::CancellationToken;

use crate::protocol::{HelloResp, DnsSettings, CLIENT_TYPE_PORTMASTER, CMD_HELLO, CMD_SET_TEMP_PRIORITIZED_DNS};

/// Timeout of a request/response round trip, like the Go client's default.
const REQUEST_TIMEOUT: Duration = Duration::from_secs(10);
/// Capacity of the event broadcast channel.
const EVENT_CHANNEL_CAPACITY: usize = 32;

/// An event reported by the IVPN client connection.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ClientEvent {
    /// A daemon message, with its command name and raw JSON payload.
    Message { name: String, data: String },
    /// The connection to the daemon was closed.
    Disconnected,
}

/// The interface the interoperability engine expects from an IVPN client.
#[async_trait]
pub trait IvpnClient: Send + Sync {
    /// Connects to the daemon. A failure is non-fatal: the Go code ignores it
    /// because the IVPN client may simply not be running.
    async fn connect(&self) -> Result<(), String>;
    /// Sends the required hello request and returns its response.
    async fn hello(&self, version: &str, name: &str) -> Result<HelloResp, String>;
    /// Sets (or, with empty settings, resets) temporary prioritized DNS.
    async fn set_temp_prioritized_dns(
        &self,
        dns: DnsSettings,
        description: &str,
    ) -> Result<(), String>;
    /// Subscribes to connection events. Call before [`IvpnClient::connect`] to
    /// avoid missing messages sent during the handshake.
    fn subscribe(&self) -> broadcast::Receiver<ClientEvent>;
    /// Closes the connection.
    fn disconnect(&self);
}

/// Creates clients and exposes the daemon port without opening a connection.
pub trait IvpnClientFactory: Send + Sync {
    /// Port and protocol secret, like Go `ivpnclient.GetConnectionPortInfo`.
    fn connection_port_info(&self) -> Result<(u16, u64), String>;
    /// Creates a fresh client for one connection attempt.
    fn create_client(&self) -> Arc<dyn IvpnClient>;
}

/// Reads the `port:secret` file written by the IVPN daemon.
pub fn read_port_info(path: &Path) -> Result<(u16, u64), String> {
    let data = std::fs::read_to_string(path)
        .map_err(|error| format!("failed to read daemon connection info: {error}"))?;
    let (port, secret) = data
        .split_once(':')
        .ok_or_else(|| "failed to parse connection-info".to_string())?;
    let port: u16 = port
        .trim()
        .parse()
        .map_err(|error| format!("failed to parse connection-info: {error}"))?;
    let secret = u64::from_str_radix(secret.trim(), 16)
        .map_err(|error| format!("failed to parse connection-info: {error}"))?;
    Ok((port, secret))
}

/// Reads the optional paranoid-mode secret file.
fn read_paranoid_secret(path: &Path) -> String {
    std::fs::read_to_string(path)
        .map(|secret| secret.trim().to_string())
        .unwrap_or_default()
}

/// TCP implementation of [`IvpnClient`].
pub struct TcpIvpnClient {
    port_file: PathBuf,
    secret_file: PathBuf,
    secret: Mutex<u64>,
    protocol_secret: Mutex<String>,
    write_half: AsyncMutex<Option<OwnedWriteHalf>>,
    events_tx: broadcast::Sender<ClientEvent>,
    waiters: Arc<Mutex<HashMap<u32, oneshot::Sender<Value>>>>,
    next_idx: AtomicU32,
    reader_cancel: Mutex<Option<CancellationToken>>,
}

impl TcpIvpnClient {
    /// Creates a client that reads daemon metadata from the given files.
    pub fn new(port_file: PathBuf, secret_file: PathBuf) -> TcpIvpnClient {
        let (events_tx, _) = broadcast::channel(EVENT_CHANNEL_CAPACITY);
        TcpIvpnClient {
            port_file,
            secret_file,
            secret: Mutex::new(0),
            protocol_secret: Mutex::new(String::new()),
            write_half: AsyncMutex::new(None),
            events_tx,
            waiters: Arc::new(Mutex::new(HashMap::new())),
            next_idx: AtomicU32::new(1),
            reader_cancel: Mutex::new(None),
        }
    }

    fn next_message_idx(&self) -> u32 {
        // Zero is reserved as "no index"; allocate with CAS so two callers
        // can never observe the same index on wraparound.
        loop {
            let current = self.next_idx.load(Ordering::Relaxed);
            let next = if current == u32::MAX { 1 } else { current + 1 };
            if self
                .next_idx
                .compare_exchange_weak(current, next, Ordering::Relaxed, Ordering::Relaxed)
                .is_ok()
            {
                return current;
            }
        }
    }

    async fn send_request(&self, command: &str, extra: Map<String, Value>) -> Result<Value, String> {
        let idx = self.next_message_idx();
        let mut request = Map::new();
        request.insert("Command".to_string(), json!(command));
        request.insert("Idx".to_string(), json!(idx));
        request.insert(
            "ProtocolSecret".to_string(),
            json!(self.protocol_secret.lock().expect("lock poisoned").clone()),
        );
        for (key, value) in extra {
            request.insert(key, value);
        }
        let mut line = serde_json::to_vec(&request)
            .map_err(|error| format!("failed to serialize command: {error}"))?;
        line.push(b'\n');

        let (sender, receiver) = oneshot::channel();
        self.waiters
            .lock()
            .expect("lock poisoned")
            .insert(idx, sender);

        let write_result = {
            let mut guard = self.write_half.lock().await;
            match guard.as_mut() {
                Some(stream) => stream.write_all(&line).await,
                None => Err(std::io::Error::new(
                    std::io::ErrorKind::NotConnected,
                    "not connected to IVPN daemon",
                )),
            }
        };
        if let Err(error) = write_result {
            self.waiters.lock().expect("lock poisoned").remove(&idx);
            return Err(format!("failed to send command: {error}"));
        }

        match tokio::time::timeout(REQUEST_TIMEOUT, receiver).await {
            Ok(Ok(value)) => Ok(value),
            Ok(Err(_)) => Err("IVPN daemon closed before answering".to_string()),
            Err(_) => {
                self.waiters.lock().expect("lock poisoned").remove(&idx);
                Err("timed out waiting for IVPN daemon response".to_string())
            }
        }
    }
}

#[async_trait]
impl IvpnClient for TcpIvpnClient {
    async fn connect(&self) -> Result<(), String> {
        let (port, secret) = read_port_info(&self.port_file)?;
        *self.secret.lock().expect("lock poisoned") = secret;
        *self.protocol_secret.lock().expect("lock poisoned") =
            read_paranoid_secret(&self.secret_file);

        let stream = TcpStream::connect(("127.0.0.1", port))
            .await
            .map_err(|error| {
                format!("failed to connect to IVPN daemon (is it running?): {error}")
            })?;
        let (read_half, write_half) = stream.into_split();
        *self.write_half.lock().await = Some(write_half);

        let cancel = CancellationToken::new();
        *self.reader_cancel.lock().expect("lock poisoned") = Some(cancel.clone());
        let events_tx = self.events_tx.clone();
        let waiters = Arc::clone(&self.waiters);
        tokio::spawn(async move {
            read_loop(read_half, events_tx, waiters, cancel).await;
        });
        Ok(())
    }

    async fn hello(&self, version: &str, name: &str) -> Result<HelloResp, String> {
        // Mirrors `ivpnclient.Client.InitHelloRequest`.
        let version_string = format!(
            "{}:{}",
            version.trim().replace(':', " "),
            name.trim().replace(':', " ")
        );
        let mut extra = Map::new();
        extra.insert("ClientType".to_string(), json!(CLIENT_TYPE_PORTMASTER));
        extra.insert("Version".to_string(), json!(version_string));
        extra.insert(
            "Secret".to_string(),
            json!(*self.secret.lock().expect("lock poisoned")),
        );
        extra.insert("GetStatus".to_string(), json!(true));
        extra.insert("GetServiceBinaryPath".to_string(), json!(true));
        extra.insert("GetActiveRemoteEndpoint".to_string(), json!(true));

        let value = self.send_request(CMD_HELLO, extra).await?;
        serde_json::from_value(value)
            .map_err(|error| format!("failed to parse hello response: {error}"))
    }

    async fn set_temp_prioritized_dns(
        &self,
        dns: DnsSettings,
        description: &str,
    ) -> Result<(), String> {
        let mut extra = Map::new();
        extra.insert(
            "Dns".to_string(),
            serde_json::to_value(dns)
                .map_err(|error| format!("failed to serialize DNS settings: {error}"))?,
        );
        extra.insert("Description".to_string(), json!(description));
        self.send_request(CMD_SET_TEMP_PRIORITIZED_DNS, extra)
            .await
            .map(|_| ())
    }

    fn subscribe(&self) -> broadcast::Receiver<ClientEvent> {
        self.events_tx.subscribe()
    }

    fn disconnect(&self) {
        if let Some(cancel) = self.reader_cancel.lock().expect("lock poisoned").take() {
            cancel.cancel();
        }
        if let Ok(mut write_half) = self.write_half.try_lock() {
            *write_half = None;
        }
        let _ = self.events_tx.send(ClientEvent::Disconnected);
    }
}

/// Reads daemon messages, resolves request waiters and broadcasts events.
async fn read_loop(
    read_half: tokio::net::tcp::OwnedReadHalf,
    events_tx: broadcast::Sender<ClientEvent>,
    waiters: Arc<Mutex<HashMap<u32, oneshot::Sender<Value>>>>,
    cancel: CancellationToken,
) {
    let mut lines = BufReader::new(read_half).lines();
    loop {
        tokio::select! {
            () = cancel.cancelled() => break,
            line = lines.next_line() => {
                let line = match line {
                    Ok(Some(line)) => line,
                    Ok(None) | Err(_) => break,
                };
                let data = line.trim();
                if data.is_empty() {
                    continue;
                }
                let Ok(value) = serde_json::from_str::<Value>(data) else {
                    continue;
                };
                let command = value
                    .get("Command")
                    .and_then(Value::as_str)
                    .unwrap_or_default()
                    .to_string();
                let idx = value
                    .get("Idx")
                    .and_then(Value::as_u64)
                    .unwrap_or_default() as u32;
                if let Some(sender) = waiters.lock().expect("lock poisoned").remove(&idx) {
                    let _ = sender.send(value);
                }
                let _ = events_tx.send(ClientEvent::Message {
                    name: command,
                    data: data.to_string(),
                });
            }
        }
    }
    let _ = events_tx.send(ClientEvent::Disconnected);
}

/// Factory for the real TCP client.
pub struct TcpIvpnClientFactory {
    port_file: PathBuf,
    secret_file: PathBuf,
}

impl TcpIvpnClientFactory {
    /// Creates a factory with explicit daemon file paths, useful in tests.
    pub fn with_paths(port_file: PathBuf, secret_file: PathBuf) -> TcpIvpnClientFactory {
        TcpIvpnClientFactory {
            port_file,
            secret_file,
        }
    }

    /// Locates the IVPN mutable directory like the Go Linux file locator:
    /// `/etc/opt/ivpn/mutable`, falling back to the snap common directory.
    pub fn system() -> TcpIvpnClientFactory {
        let base = if Path::new("/etc/opt/ivpn/mutable").is_dir() {
            PathBuf::from("/etc/opt/ivpn/mutable")
        } else {
            PathBuf::from("/var/snap/ivpn/common/opt/ivpn/mutable")
        };
        TcpIvpnClientFactory {
            port_file: base.join("port.txt"),
            secret_file: base.join("eaa"),
        }
    }
}

impl IvpnClientFactory for TcpIvpnClientFactory {
    fn connection_port_info(&self) -> Result<(u16, u64), String> {
        read_port_info(&self.port_file)
    }

    fn create_client(&self) -> Arc<dyn IvpnClient> {
        Arc::new(TcpIvpnClient::new(
            self.port_file.clone(),
            self.secret_file.clone(),
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_file(name: &str, contents: &str) -> PathBuf {
        let path = std::env::temp_dir().join(format!("pm-interop-test-{name}-{}", std::process::id()));
        std::fs::write(&path, contents).expect("write temp file");
        path
    }

    #[test]
    fn parses_daemon_port_file() {
        let path = temp_file("port", " 24500 : 1a2b3c\n");
        let (port, secret) = read_port_info(&path).expect("parse");
        assert_eq!(port, 24500);
        assert_eq!(secret, 0x1a2b3c);
        std::fs::remove_file(path).ok();
    }

    #[test]
    fn rejects_invalid_daemon_port_file() {
        let path = temp_file("port-bad", "no-separator");
        assert!(read_port_info(&path).is_err());
        std::fs::remove_file(path).ok();
        let path = temp_file("port-bad-hex", "24500:zz");
        assert!(read_port_info(&path).is_err());
        std::fs::remove_file(path).ok();
    }

    #[test]
    fn message_indices_never_start_at_zero() {
        let client = TcpIvpnClient::new(PathBuf::new(), PathBuf::new());
        let first = client.next_message_idx();
        assert_ne!(first, 0);
        assert_eq!(client.next_message_idx(), first + 1);
    }
}
