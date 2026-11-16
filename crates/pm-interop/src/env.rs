//! Injectable environment of the interoperability module.
//!
//! The Go module reaches into package globals (config, interception, netenv,
//! the notification database and the module manager). These are collected in
//! one trait so the engine can be exercised with a mock in tests and with the
//! real core services in production.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use pm_notifications::Notifications;
use tokio::sync::broadcast;

/// Everything the interoperability engine reads from or writes to the core.
pub trait InteropEnv: Send + Sync {
    /// Portmaster's local DNS resolver address, like Go `DnsListenAddress`.
    fn dns_listen_address(&self) -> String;
    /// Configured custom DNS servers, like Go `DnsNameServers`.
    fn dns_name_servers(&self) -> Vec<String>;
    /// Whether packet interception is active (PM not paused).
    fn interception_started(&self) -> bool;
    /// Stream of interception start/stop events.
    fn subscribe_interception(&self) -> broadcast::Receiver<()>;
    /// Stream of configuration changes.
    fn subscribe_config_change(&self) -> broadcast::Receiver<()>;
    /// Lazily registers the external verdict handler, like the Go owner method.
    fn ensure_verdict_handler_registered(&self);
    /// Enables/disables network-derived location sources while a VPN is active.
    fn set_network_derived_location_disabled(&self, disable: bool);
    /// Whether the user chose to never see the IVPN detected notification.
    fn is_notification_suppressed(&self) -> bool;
    /// Persists the user's choice to suppress the notification.
    fn suppress_notification(&self) -> Result<(), String>;
    /// The core notification registry.
    fn notifications(&self) -> &Arc<Notifications>;
    /// Portmaster version reported to the IVPN daemon.
    fn version(&self) -> String;
}

/// In-memory environment used by unit tests.
pub struct TestEnv {
    pub dns_listen: Mutex<String>,
    pub dns_servers: Mutex<Vec<String>>,
    pub interception: AtomicBool,
    pub location_disabled: AtomicBool,
    pub suppressed: AtomicBool,
    pub verdict_registered: AtomicBool,
    pub notifications: Arc<Notifications>,
    pub interception_tx: broadcast::Sender<()>,
    pub config_tx: broadcast::Sender<()>,
}

impl Default for TestEnv {
    fn default() -> TestEnv {
        let (interception_tx, _) = broadcast::channel(8);
        let (config_tx, _) = broadcast::channel(8);
        TestEnv {
            dns_listen: Mutex::new(String::new()),
            dns_servers: Mutex::new(Vec::new()),
            interception: AtomicBool::new(false),
            location_disabled: AtomicBool::new(false),
            suppressed: AtomicBool::new(false),
            verdict_registered: AtomicBool::new(false),
            notifications: Arc::new(Notifications::new()),
            interception_tx,
            config_tx,
        }
    }
}

impl TestEnv {
    /// Creates an environment with a fresh notification registry.
    pub fn new() -> TestEnv {
        let (interception_tx, _) = broadcast::channel(8);
        let (config_tx, _) = broadcast::channel(8);
        TestEnv {
            notifications: Arc::new(Notifications::new()),
            interception_tx,
            config_tx,
            ..TestEnv::default()
        }
    }

    /// Sets the DNS listen address.
    pub fn with_dns_listen(mut self, address: &str) -> TestEnv {
        *self.dns_listen.get_mut().expect("lock poisoned") = address.to_string();
        self
    }

    /// Sets the configured custom DNS servers.
    pub fn with_dns_servers(self, servers: &[&str]) -> TestEnv {
        *self.dns_servers.lock().expect("lock poisoned") =
            servers.iter().map(|value| (*value).to_string()).collect();
        self
    }

    /// Sets whether interception is active.
    pub fn with_interception(self, started: bool) -> TestEnv {
        self.interception.store(started, Ordering::SeqCst);
        self
    }
}

impl InteropEnv for TestEnv {
    fn dns_listen_address(&self) -> String {
        self.dns_listen.lock().expect("lock poisoned").clone()
    }

    fn dns_name_servers(&self) -> Vec<String> {
        self.dns_servers.lock().expect("lock poisoned").clone()
    }

    fn interception_started(&self) -> bool {
        self.interception.load(Ordering::SeqCst)
    }

    fn subscribe_interception(&self) -> broadcast::Receiver<()> {
        self.interception_tx.subscribe()
    }

    fn subscribe_config_change(&self) -> broadcast::Receiver<()> {
        self.config_tx.subscribe()
    }

    fn ensure_verdict_handler_registered(&self) {
        self.verdict_registered.store(true, Ordering::SeqCst);
    }

    fn set_network_derived_location_disabled(&self, disable: bool) {
        self.location_disabled.store(disable, Ordering::SeqCst);
    }

    fn is_notification_suppressed(&self) -> bool {
        self.suppressed.load(Ordering::SeqCst)
    }

    fn suppress_notification(&self) -> Result<(), String> {
        self.suppressed.store(true, Ordering::SeqCst);
        Ok(())
    }

    fn notifications(&self) -> &Arc<Notifications> {
        &self.notifications
    }

    fn version(&self) -> String {
        "1.0.0".to_string()
    }
}
