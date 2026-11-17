//! Process manager tests through a fake process adapter (no real binary).

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};

use pm_xray::config::InboundProtocol;
use pm_xray::{ChildHandle, ProcessAdapter, ProcessState, XrayManager, XrayNode};

#[derive(Default)]
struct FakeProcess {
    exited: Arc<AtomicBool>,
    killed: Arc<AtomicBool>,
}

impl ChildHandle for FakeProcess {
    fn id(&self) -> u32 {
        4321
    }
    fn try_wait(&mut self) -> Result<Option<i32>, String> {
        Ok(if self.exited.load(Ordering::SeqCst) {
            Some(0)
        } else {
            None
        })
    }
    fn kill(&mut self) -> Result<(), String> {
        self.killed.store(true, Ordering::SeqCst);
        Ok(())
    }
    fn wait(&mut self) -> Result<i32, String> {
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
    fn spawn(&self, binary: &str, config: &Path) -> Result<Box<dyn ChildHandle>, String> {
        if self.fail {
            return Err("fake failure".into());
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
        subscription_id: "sub-1".into(),
        name: format!("Node {id}"),
        country_code: String::new(),
        country_name: String::new(),
        latitude: None,
        longitude: None,
        protocol: pm_xray::Protocol::Vless,
        address: "example.com".into(),
        port: 443,
        raw_config: "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443".into(),
    }
}

type SpawnLog = Arc<Mutex<Vec<(String, PathBuf)>>>;

struct Harness {
    manager: XrayManager,
    exited: Arc<AtomicBool>,
    killed: Arc<AtomicBool>,
    spawns: SpawnLog,
}

fn manager(fail: bool) -> Harness {
    let adapter = FakeAdapter {
        spawns: Arc::new(Mutex::new(Vec::new())),
        fail,
        exited: Arc::new(AtomicBool::new(false)),
        killed: Arc::new(AtomicBool::new(false)),
    };
    let (exited, killed, spawns) = (
        Arc::clone(&adapter.exited),
        Arc::clone(&adapter.killed),
        Arc::clone(&adapter.spawns),
    );
    static NEXT_DIR: AtomicU64 = AtomicU64::new(0);
    let unique = NEXT_DIR.fetch_add(1, Ordering::SeqCst);
    let dir = std::env::temp_dir().join(format!(
        "pm-xray-manager-it-{}-{unique}",
        std::process::id()
    ));
    let _ = std::fs::remove_dir_all(&dir);
    Harness {
        manager: XrayManager::new("/opt/bin/xray-rust".into(), dir, Box::new(adapter)),
        exited,
        killed,
        spawns,
    }
}

#[test]
fn spawns_restarts_tracks_state_and_kills_child() {
    let Harness {
        manager,
        exited,
        killed,
        spawns,
    } = manager(false);
    assert_eq!(manager.status().state, ProcessState::Stopped);
    assert!(manager.status().binary_available);

    manager
        .connect(&node("one"), 1080, InboundProtocol::Socks)
        .unwrap();
    let status = manager.status();
    assert_eq!(status.state, ProcessState::Running);
    assert_eq!(status.node_id.as_deref(), Some("one"));
    assert_eq!(status.inbound_address.as_deref(), Some("127.0.0.1:1080"));
    assert_eq!(spawns.lock().unwrap().len(), 1);
    assert!(spawns.lock().unwrap()[0].1.exists());

    // Switching nodes kills the old child and writes a new config.
    manager
        .connect(&node("two"), 1081, InboundProtocol::Http)
        .unwrap();
    assert!(killed.load(Ordering::SeqCst));
    assert_eq!(spawns.lock().unwrap().len(), 2);

    // A child exiting on its own is reported as failed.
    exited.store(true, Ordering::SeqCst);
    assert_eq!(manager.status().state, ProcessState::Failed);
    assert!(manager.status().error.is_some());

    manager.stop();
    assert_eq!(manager.status().state, ProcessState::Stopped);
    assert!(manager.status().node_id.is_none());
}

#[test]
fn spawn_failure_marks_process_failed() {
    let Harness { manager, spawns, .. } = manager(true);
    assert!(manager
        .connect(&node("one"), 1080, InboundProtocol::Socks)
        .is_err());
    assert_eq!(manager.status().state, ProcessState::Failed);
    assert!(spawns.lock().unwrap().is_empty());
}

#[test]
fn missing_binary_is_reported_without_panicking() {
    let adapter = FakeAdapter {
        spawns: Arc::new(Mutex::new(Vec::new())),
        fail: false,
        exited: Arc::new(AtomicBool::new(false)),
        killed: Arc::new(AtomicBool::new(false)),
    };
    let manager = XrayManager::new(String::new(), std::env::temp_dir(), Box::new(adapter));
    assert!(!manager.status().binary_available);
    assert!(manager
        .connect(&node("one"), 1080, InboundProtocol::Socks)
        .is_err());
}
