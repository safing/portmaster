//! Child-process lifecycle management for `xray-rust`.
//!
//! The binary is launched as `xray-rust run -config <config.json>`. Process
//! output is forwarded to `tracing`; the child is killed on disconnect, on
//! shutdown and when a different node is selected.

use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use std::thread;

use crate::config::{write_config, InboundProtocol};
use crate::model::{ProcessState, XrayNode, XrayStatus};
use crate::{Error, Result};

/// Minimal handle over a spawned child, so tests can use a fake process.
pub trait ChildHandle: Send {
    fn id(&self) -> u32;
    fn try_wait(&mut self) -> std::result::Result<Option<i32>, String>;
    fn kill(&mut self) -> std::result::Result<(), String>;
    fn wait(&mut self) -> std::result::Result<i32, String>;
}

/// Injectable process spawner.
pub trait ProcessAdapter: Send + Sync {
    fn spawn(&self, binary: &str, config: &Path) -> std::result::Result<Box<dyn ChildHandle>, String>;
}

/// Adapter that actually starts the Xray binary.
pub struct SystemProcessAdapter;

struct SystemChild {
    child: Child,
    id: u32,
}

impl ChildHandle for SystemChild {
    fn id(&self) -> u32 {
        self.id
    }

    fn try_wait(&mut self) -> std::result::Result<Option<i32>, String> {
        self.child
            .try_wait()
            .map(|status| status.and_then(|status| status.code()))
            .map_err(|error| error.to_string())
    }

    fn kill(&mut self) -> std::result::Result<(), String> {
        self.child.kill().map_err(|error| error.to_string())
    }

    fn wait(&mut self) -> std::result::Result<i32, String> {
        self.child
            .wait()
            .map(|status| status.code().unwrap_or(-1))
            .map_err(|error| error.to_string())
    }
}

impl ProcessAdapter for SystemProcessAdapter {
    fn spawn(&self, binary: &str, config: &Path) -> std::result::Result<Box<dyn ChildHandle>, String> {
        let mut child = Command::new(binary)
            .arg("run")
            .arg("-config")
            .arg(config)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|error| format!("failed to start {binary}: {error}"))?;
        let id = child.id();
        if let Some(stdout) = child.stdout.take() {
            thread::spawn(move || {
                for line in BufReader::new(stdout).lines().map_while(std::result::Result::ok) {
                    tracing::info!(target: "xray", pid = id, "{line}");
                }
            });
        }
        if let Some(stderr) = child.stderr.take() {
            thread::spawn(move || {
                for line in BufReader::new(stderr).lines().map_while(std::result::Result::ok) {
                    tracing::warn!(target: "xray", pid = id, "{line}");
                }
            });
        }
        Ok(Box::new(SystemChild { child, id }))
    }
}

/// Kills the child if the manager is dropped without an explicit disconnect.
struct ChildGuard {
    child: Option<Box<dyn ChildHandle>>,
}

impl Drop for ChildGuard {
    fn drop(&mut self) {
        if let Some(child) = self.child.as_mut() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }
}

struct ManagerState {
    state: ProcessState,
    node: Option<XrayNode>,
    child: Option<ChildGuard>,
    error: Option<String>,
    inbound_port: u16,
    inbound: InboundProtocol,
    inbound_address: Option<String>,
}

impl ManagerState {
    fn stop_child(&mut self) {
        if let Some(mut guard) = self.child.take() {
            if let Some(child) = guard.child.as_mut() {
                let _ = child.kill();
                let _ = child.wait();
            }
        }
    }
}

/// Manages one Xray child at a time.
pub struct XrayManager {
    binary: String,
    data_dir: PathBuf,
    inner: Mutex<ManagerState>,
    adapter: Box<dyn ProcessAdapter>,
}

impl XrayManager {
    pub fn new(binary: String, data_dir: PathBuf, adapter: Box<dyn ProcessAdapter>) -> XrayManager {
        XrayManager {
            binary,
            data_dir,
            inner: Mutex::new(ManagerState {
                state: ProcessState::Stopped,
                node: None,
                child: None,
                error: None,
                inbound_port: 1080,
                inbound: InboundProtocol::Socks,
                inbound_address: None,
            }),
            adapter,
        }
    }

    /// Creates a manager using the real process adapter.
    pub fn system(binary: String, data_dir: PathBuf) -> XrayManager {
        Self::new(binary, data_dir, Box::new(SystemProcessAdapter))
    }

    pub fn binary(&self) -> &str {
        &self.binary
    }

    pub fn binary_available(&self) -> bool {
        !self.binary.is_empty()
    }

    /// Connects to `node`, replacing any running process (restart on change).
    pub fn connect(&self, node: &XrayNode, inbound_port: u16, inbound: InboundProtocol) -> Result<()> {
        if self.binary.is_empty() {
            return Err(Error::Process(
                "xray binary is not configured or was not found".into(),
            ));
        }
        let config = write_config(&self.data_dir, node, inbound_port, inbound)?;
        let mut state = self.inner.lock().expect("lock poisoned");
        state.stop_child();
        state.state = ProcessState::Starting;
        state.node = Some(node.clone());
        state.error = None;
        state.inbound_port = inbound_port;
        state.inbound = inbound;
        state.inbound_address = None;

        match self.adapter.spawn(&self.binary, &config) {
            Ok(child) => {
                tracing::info!(pid = child.id(), config = %config.display(), "xray: process started");
                state.child = Some(ChildGuard { child: Some(child) });
                state.state = ProcessState::Running;
                state.inbound_address = Some(format!("127.0.0.1:{inbound_port}"));
                Ok(())
            }
            Err(error) => {
                state.state = ProcessState::Failed;
                state.error = Some(error.clone());
                Err(Error::Process(error))
            }
        }
    }

    /// Stops the process and clears the selected node.
    pub fn disconnect(&self) -> Result<()> {
        let mut state = self.inner.lock().expect("lock poisoned");
        state.stop_child();
        state.state = ProcessState::Stopped;
        state.node = None;
        state.error = None;
        state.inbound_address = None;
        Ok(())
    }

    /// Restarts the current node with its previous inbound settings.
    pub fn restart(&self) -> Result<()> {
        let (node, port, inbound) = {
            let state = self.inner.lock().expect("lock poisoned");
            let node = state.node.clone().ok_or_else(|| Error::Process("no node selected".into()))?;
            (node, state.inbound_port, state.inbound)
        };
        self.connect(&node, port, inbound)
    }

    /// Returns the current status, noticing when a child exited by itself.
    pub fn status(&self) -> XrayStatus {
        let mut state = self.inner.lock().expect("lock poisoned");
        if state.state == ProcessState::Running {
            let mut exited = None;
            if let Some(guard) = state.child.as_mut() {
                if let Some(child) = guard.child.as_mut() {
                    match child.try_wait() {
                        Ok(Some(code)) => exited = Some(format!("process exited with status {code}")),
                        Ok(None) => {}
                        Err(error) => exited = Some(format!("process status check failed: {error}")),
                    }
                }
            }
            if let Some(error) = exited {
                state.state = ProcessState::Failed;
                state.error = Some(error);
                state.child = None;
                state.inbound_address = None;
            }
        }
        XrayStatus {
            enabled: self.binary_available(),
            binary: self.binary.clone(),
            binary_available: self.binary_available(),
            state: state.state,
            node_id: state.node.as_ref().map(|node| node.id.clone()),
            node_name: state.node.as_ref().map(|node| node.name.clone()),
            inbound_address: state.inbound_address.clone(),
            error: state.error.clone(),
        }
    }

    /// Idempotent shutdown helper used by the core.
    pub fn stop(&self) {
        if let Err(error) = self.disconnect() {
            tracing::warn!(%error, "xray: failed to stop process");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
    use std::sync::Arc;

    #[derive(Default)]
    struct FakeProcess {
        exited: Arc<AtomicBool>,
        killed: Arc<AtomicBool>,
    }

    impl ChildHandle for FakeProcess {
        fn id(&self) -> u32 {
            4242
        }
        fn try_wait(&mut self) -> std::result::Result<Option<i32>, String> {
            Ok(if self.exited.load(Ordering::SeqCst) { Some(0) } else { None })
        }
        fn kill(&mut self) -> std::result::Result<(), String> {
            self.killed.store(true, Ordering::SeqCst);
            Ok(())
        }
        fn wait(&mut self) -> std::result::Result<i32, String> {
            Ok(0)
        }
    }

    struct FakeAdapter {
        spawns: Arc<Mutex<Vec<(String, PathBuf)>>>,
        fail: bool,
        exited: Arc<AtomicBool>,
        killed: Arc<AtomicBool>,
    }

    impl ProcessAdapter for FakeAdapter {
        fn spawn(&self, binary: &str, config: &Path) -> std::result::Result<Box<dyn ChildHandle>, String> {
            if self.fail {
                return Err("fake spawn failure".into());
            }
            self.spawns
                .lock()
                .unwrap()
                .push((binary.to_string(), config.to_path_buf()));
            Ok(Box::new(FakeProcess {
                exited: Arc::clone(&self.exited),
                killed: Arc::clone(&self.killed),
            }))
        }
    }

    fn node(id: &str) -> XrayNode {
        XrayNode {
            id: id.into(),
            subscription_id: "sub".into(),
            name: format!("Node {id}"),
            country_code: String::new(),
            country_name: String::new(),
            protocol: crate::model::Protocol::Vless,
            address: "example.com".into(),
            port: 443,
            raw_config: "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443".into(),
        }
    }

    static NEXT_DIR: AtomicU64 = AtomicU64::new(0);

    type SpawnLog = Arc<Mutex<Vec<(String, PathBuf)>>>;

    struct Harness {
        manager: XrayManager,
        exited: Arc<AtomicBool>,
        killed: Arc<AtomicBool>,
        spawns: SpawnLog,
    }

    fn manager(adapter: FakeAdapter) -> Harness {
        let (exited, killed, spawns) = (Arc::clone(&adapter.exited), Arc::clone(&adapter.killed), Arc::clone(&adapter.spawns));
        let unique = NEXT_DIR.fetch_add(1, Ordering::SeqCst)
            ^ (std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .subsec_nanos() as u64);
        let data_dir = std::env::temp_dir().join(format!("pm-xray-manager-test-{}-{unique}", std::process::id()));
        let _ = std::fs::remove_dir_all(&data_dir);
        Harness {
            manager: XrayManager::new("/usr/bin/xray-rust".into(), data_dir, Box::new(adapter)),
            exited,
            killed,
            spawns,
        }
    }

    #[test]
    fn connects_disconnects_and_restarts() {
        let adapter = FakeAdapter {
            spawns: Arc::new(Mutex::new(Vec::new())),
            fail: false,
            exited: Arc::new(AtomicBool::new(false)),
            killed: Arc::new(AtomicBool::new(false)),
        };
        let Harness { manager, exited, killed, spawns } = manager(adapter);
        manager.connect(&node("one"), 1080, InboundProtocol::Socks).unwrap();
        assert_eq!(manager.status().state, ProcessState::Running);
        assert_eq!(manager.status().inbound_address.as_deref(), Some("127.0.0.1:1080"));
        // Selecting a different node restarts (kills old, spawns new).
        manager.connect(&node("two"), 1081, InboundProtocol::Http).unwrap();
        assert!(killed.load(Ordering::SeqCst));
        assert_eq!(spawns.lock().unwrap().len(), 2);
        assert_eq!(spawns.lock().unwrap()[1].0, "/usr/bin/xray-rust");
        assert!(spawns.lock().unwrap()[1].1.exists());
        exited.store(true, Ordering::SeqCst);
        assert_eq!(manager.status().state, ProcessState::Failed);
        manager.disconnect().unwrap();
        assert_eq!(manager.status().state, ProcessState::Stopped);
        assert!(manager.status().node_id.is_none());
    }

    #[test]
    fn reports_spawn_failure() {
        let adapter = FakeAdapter {
            spawns: Arc::new(Mutex::new(Vec::new())),
            fail: true,
            exited: Arc::new(AtomicBool::new(false)),
            killed: Arc::new(AtomicBool::new(false)),
        };
        let Harness { manager, .. } = manager(adapter);
        assert!(manager.connect(&node("one"), 1080, InboundProtocol::Socks).is_err());
        assert_eq!(manager.status().state, ProcessState::Failed);
        assert!(manager.status().error.is_some());
    }
}
