//! Tests for the network change monitor (Go `netenv/network-change.go`).

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_netenv::{
    global_source, InterfaceSource, NetEnv, NetworkChangeMonitor, RawInterface, RouteCandidate,
};

struct FakeSource {
    interfaces: Mutex<Vec<RawInterface>>,
}

impl FakeSource {
    fn new() -> Arc<FakeSource> {
        Arc::new(FakeSource {
            interfaces: Mutex::new(vec![RawInterface {
                name: "eth0".to_string(),
                index: 1,
                mac: Some("aa:bb:cc:dd:ee:ff".to_string()),
                up: true,
                loopback: false,
                addrs: vec![("192.168.1.10".parse().unwrap(), 24)],
            }]),
        })
    }
}

impl InterfaceSource for FakeSource {
    fn interfaces(&self) -> Result<Vec<RawInterface>, String> {
        Ok(self.interfaces.lock().expect("lock").clone())
    }

    fn is_physical(&self, _name: &str) -> bool {
        true
    }

    fn ipv4_default_routes(&self) -> Result<Vec<RouteCandidate>, String> {
        Ok(Vec::new())
    }

    fn ipv6_default_routes(&self) -> Vec<RouteCandidate> {
        Vec::new()
    }
}

#[test]
fn detects_interface_changes_once() {
    let source = FakeSource::new();
    let online_checks = Arc::new(AtomicUsize::new(0));
    let counter = Arc::clone(&online_checks);
    let monitor = NetworkChangeMonitor::new(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        Arc::new(NetEnv::new()),
        Some(Arc::new(move || {
            counter.fetch_add(1, Ordering::SeqCst);
        })),
    );

    // The first check only records the checksum.
    assert!(!monitor.check_once().expect("first"));
    assert!(!monitor.check_once().expect("unchanged"));
    assert_eq!(online_checks.load(Ordering::SeqCst), 0);

    // A new address is a change.
    let generation = global_source().generation();
    source.interfaces.lock().unwrap()[0]
        .addrs
        .push(("192.168.1.11".parse().unwrap(), 24));
    assert!(monitor.check_once().expect("changed"));
    assert_eq!(online_checks.load(Ordering::SeqCst), 1);
    assert_eq!(
        global_source().generation(),
        generation + 1,
        "network change was announced"
    );

    // The same state no longer counts as a change.
    assert!(!monitor.check_once().expect("stable"));
    assert_eq!(online_checks.load(Ordering::SeqCst), 1);
}

#[test]
fn manual_triggers_skip_the_online_check() {
    let source = FakeSource::new();
    let online_checks = Arc::new(AtomicUsize::new(0));
    let counter = Arc::clone(&online_checks);
    let monitor = NetworkChangeMonitor::new(
        Arc::clone(&source) as Arc<dyn InterfaceSource>,
        Arc::new(NetEnv::new()),
        Some(Arc::new(move || {
            counter.fetch_add(1, Ordering::SeqCst);
        })),
    );

    monitor.check_once().expect("record checksum");
    source.interfaces.lock().unwrap()[0].up = false;
    assert!(monitor.check_once_triggered().expect("changed"));
    assert_eq!(
        online_checks.load(Ordering::SeqCst),
        0,
        "manual trigger does not investigate"
    );
}

#[tokio::test]
async fn run_stops_on_cancellation() {
    let monitor = Arc::new(NetworkChangeMonitor::new(
        FakeSource::new() as Arc<dyn InterfaceSource>,
        Arc::new(NetEnv::new()),
        None,
    ));
    let token = tokio_util::sync::CancellationToken::new();
    let task = {
        let monitor = Arc::clone(&monitor);
        let token = token.clone();
        tokio::spawn(async move { monitor.run(token).await })
    };

    // Let the loop start, then stop it.
    tokio::time::sleep(Duration::from_millis(20)).await;
    token.cancel();
    tokio::time::timeout(Duration::from_secs(2), task)
        .await
        .expect("monitor stops")
        .expect("task");
}
