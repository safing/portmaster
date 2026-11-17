//! Injectable source of systemd-resolved varlink events.
//!
//! The production adapter speaks the NUL-delimited varlink protocol over the
//! systemd-resolved Unix socket. Tests use [`FakeEventSource`], which needs
//! neither systemd nor root privileges.

use std::collections::VecDeque;
use std::io;
use std::path::PathBuf;
use std::sync::Arc;

#[cfg(unix)]
use std::sync::Mutex;

#[cfg(unix)]
use std::io::{BufRead, BufReader, Write};
#[cfg(unix)]
use std::net::Shutdown;
#[cfg(unix)]
use std::os::unix::net::UnixStream;

/// Unix socket of the systemd-resolved query monitor (Go
/// `io.systemd.Resolve.Monitor`).
pub const DEFAULT_SYSTEMD_SOCKET: &str = "/run/systemd/resolve/io.systemd.Resolve.Monitor";
/// Varlink method called to subscribe to query results.
pub const SUBSCRIBE_METHOD: &str = "io.systemd.Resolve.Monitor.SubscribeQueryResults";

/// Error of an event source.
#[derive(Debug, thiserror::Error)]
pub enum SourceError {
    /// The socket is missing; the system has no systemd-resolved monitor.
    #[error("system does not support systemd resolver monitor: {0}")]
    Unavailable(String),
    /// The socket could not be opened or read.
    #[error("failed to connect to systemd-resolver varlink service: {0}")]
    Io(#[from] io::Error),
    /// The varlink call or the daemon reply failed.
    #[error("failed to issue Varlink call: {0}")]
    Varlink(String),
}

/// Callback that unblocks a source stuck in [`DnsEventSource::recv`].
pub type ShutdownFn = Arc<dyn Fn() + Send + Sync>;

/// Source of raw varlink messages. One instance owns one connection and is
/// consumed by the monitor thread.
pub trait DnsEventSource: Send {
    /// Subscribes to query results; a failure here is a missing or broken
    /// socket.
    fn subscribe(&mut self) -> Result<(), SourceError>;

    /// Blocks until the next varlink message arrives. `Ok(None)` means the
    /// stream ended.
    fn recv(&mut self) -> Result<Option<String>, SourceError>;

    /// Returns a callback that shuts the connection down from another thread.
    /// Sources that do not block may return `None`.
    fn shutdown_fn(&self) -> Option<ShutdownFn> {
        None
    }
}

/// Production source backed by the systemd-resolved varlink socket.
#[cfg(unix)]
pub struct SystemdVarlinkSource {
    path: PathBuf,
    reader: Option<BufReader<UnixStream>>,
    writer: Option<UnixStream>,
    /// A clone of the connected stream, used to interrupt a blocking read.
    shutdown_socket: Arc<Mutex<Option<UnixStream>>>,
}

#[cfg(unix)]
impl Default for SystemdVarlinkSource {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(unix)]
impl SystemdVarlinkSource {
    /// Uses the standard systemd-resolved monitor socket.
    pub fn new() -> SystemdVarlinkSource {
        Self::with_path(DEFAULT_SYSTEMD_SOCKET)
    }

    /// Uses a custom socket path, also useful for tests with a real socket.
    pub fn with_path(path: impl Into<PathBuf>) -> SystemdVarlinkSource {
        SystemdVarlinkSource {
            path: path.into(),
            reader: None,
            writer: None,
            shutdown_socket: Arc::new(Mutex::new(None)),
        }
    }

    /// Returns the configured socket path.
    pub fn path(&self) -> &std::path::Path {
        &self.path
    }
}

#[cfg(unix)]
impl DnsEventSource for SystemdVarlinkSource {
    fn subscribe(&mut self) -> Result<(), SourceError> {
        if std::fs::metadata(&self.path).is_err() {
            return Err(SourceError::Unavailable(self.path.display().to_string()));
        }

        let stream = UnixStream::connect(&self.path)?;
        let writer = stream.try_clone()?;
        let mut writer = writer;
        *self
            .shutdown_socket
            .lock()
            .unwrap_or_else(|error| error.into_inner()) = Some(stream.try_clone()?);

        let call = serde_json::json!({
            "method": SUBSCRIBE_METHOD,
            "parameters": {},
            "more": true,
        });
        serde_json::to_writer(&mut writer, &call)
            .map_err(|err| SourceError::Varlink(err.to_string()))?;
        writer
            .write_all(b"\0")
            .map_err(|err| SourceError::Varlink(err.to_string()))?;
        writer.flush()?;

        self.reader = Some(BufReader::new(stream));
        self.writer = Some(writer);
        Ok(())
    }

    fn shutdown_fn(&self) -> Option<ShutdownFn> {
        let socket = Arc::clone(&self.shutdown_socket);
        Some(Arc::new(move || {
            if let Some(stream) = socket.lock().ok().and_then(|mut s| s.take()) {
                let _ = stream.shutdown(Shutdown::Both);
            }
        }))
    }

    fn recv(&mut self) -> Result<Option<String>, SourceError> {
        let Some(reader) = self.reader.as_mut() else {
            return Err(SourceError::Varlink("not subscribed".into()));
        };

        loop {
            let mut buf = Vec::new();
            if reader.read_until(0, &mut buf)? == 0 {
                return Ok(None);
            }
            if buf.last() == Some(&0) {
                buf.pop();
            }
            if buf.is_empty() {
                continue;
            }
            return String::from_utf8(buf)
                .map(Some)
                .map_err(|err| SourceError::Varlink(format!("invalid UTF-8: {err}")));
        }
    }
}

/// Non-Unix stub for the systemd-resolved varlink source.
///
/// The systemd monitor does not exist outside Unix, so this source is always
/// unavailable. It keeps the public API available for cross-compilation.
#[cfg(not(unix))]
pub struct SystemdVarlinkSource {
    path: PathBuf,
}

#[cfg(not(unix))]
impl Default for SystemdVarlinkSource {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(not(unix))]
impl SystemdVarlinkSource {
    /// Uses the standard systemd-resolved monitor socket path.
    pub fn new() -> SystemdVarlinkSource {
        Self::with_path(DEFAULT_SYSTEMD_SOCKET)
    }

    /// Uses a custom socket path.
    pub fn with_path(path: impl Into<PathBuf>) -> SystemdVarlinkSource {
        SystemdVarlinkSource { path: path.into() }
    }

    /// Returns the configured socket path.
    pub fn path(&self) -> &std::path::Path {
        &self.path
    }
}

#[cfg(not(unix))]
impl DnsEventSource for SystemdVarlinkSource {
    fn subscribe(&mut self) -> Result<(), SourceError> {
        Err(SourceError::Unavailable(self.path.display().to_string()))
    }

    fn recv(&mut self) -> Result<Option<String>, SourceError> {
        Err(SourceError::Varlink("not subscribed".into()))
    }
}

/// Test source that replays prepared raw messages and can simulate a failing
/// subscription.
#[derive(Default)]
pub struct FakeEventSource {
    events: VecDeque<String>,
    subscribe_error: Option<String>,
}

impl FakeEventSource {
    /// Creates a source replaying the given raw varlink messages.
    pub fn new(events: impl IntoIterator<Item = String>) -> FakeEventSource {
        FakeEventSource {
            events: events.into_iter().collect(),
            subscribe_error: None,
        }
    }

    /// Creates a source whose subscription fails with the given message.
    pub fn failing(message: impl Into<String>) -> FakeEventSource {
        FakeEventSource {
            events: VecDeque::new(),
            subscribe_error: Some(message.into()),
        }
    }

    /// Appends a raw message to the replay queue.
    pub fn push(&mut self, event: impl Into<String>) {
        self.events.push_back(event.into());
    }
}

impl DnsEventSource for FakeEventSource {
    fn subscribe(&mut self) -> Result<(), SourceError> {
        match &self.subscribe_error {
            Some(message) => Err(SourceError::Varlink(message.clone())),
            None => Ok(()),
        }
    }

    fn recv(&mut self) -> Result<Option<String>, SourceError> {
        Ok(self.events.pop_front())
    }
}
