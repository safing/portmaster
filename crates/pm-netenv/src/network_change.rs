//! Network change monitor, port of Go `service/netenv/network-change.go`.
//!
//! A checksum over the interface list (names, state and addresses) is compared
//! to the previous one. Native changes notify the network change source and
//! trigger an online status investigation; manual triggers skip the online
//! check, like the Go worker.

use std::sync::{Arc, Mutex};
use std::time::Duration;

use sha1::Digest;
use tokio::sync::Notify;
use tokio_util::sync::CancellationToken;

use crate::{notify_network_change, InterfaceSource, NetEnv};

/// Monitor interval while online.
pub const ONLINE_CHECK_INTERVAL: Duration = Duration::from_secs(15);
/// Monitor interval while offline.
pub const OFFLINE_CHECK_INTERVAL: Duration = Duration::from_secs(1);

/// Go's `net.Flags.String()` restricted to the flags this port tracks.
fn flags_string(up: bool, loopback: bool) -> String {
    let mut flags = Vec::new();
    if up {
        flags.push("up");
    }
    if loopback {
        flags.push("loopback");
    }
    if flags.is_empty() {
        "0".to_string()
    } else {
        flags.join("|")
    }
}

/// Watches the network interfaces for changes.
pub struct NetworkChangeMonitor {
    source: Arc<dyn InterfaceSource>,
    netenv: Arc<NetEnv>,
    last_checksum: Mutex<Option<Vec<u8>>>,
    trigger: Notify,
    online_trigger: Option<Arc<dyn Fn() + Send + Sync>>,
}

impl NetworkChangeMonitor {
    /// Creates the monitor. `online_trigger` is called when a native change is
    /// detected, like Go `TriggerOnlineStatusInvestigation`.
    pub fn new(
        source: Arc<dyn InterfaceSource>,
        netenv: Arc<NetEnv>,
        online_trigger: Option<Arc<dyn Fn() + Send + Sync>>,
    ) -> NetworkChangeMonitor {
        NetworkChangeMonitor {
            source,
            netenv,
            last_checksum: Mutex::new(None),
            trigger: Notify::new(),
            online_trigger,
        }
    }

    /// Triggers a check, like Go `TriggerNetworkChangeCheck`.
    pub fn trigger(&self) {
        self.trigger.notify_one();
    }

    /// Computes the checksum of the current interfaces.
    fn checksum(&self) -> Result<Vec<u8>, String> {
        let interfaces = self.source.interfaces()?;
        let mut hasher = sha1::Sha1::new();
        for interface in interfaces {
            hasher.update(interface.name.as_bytes());
            // Go hashes `iface.Flags.String()`. The system source tracks only
            // up and loopback, so build the equivalent flag string. The MAC is
            // deliberately not hashed, matching Go.
            hasher.update(flags_string(interface.up, interface.loopback).as_bytes());
            for (ip, prefix) in interface.addrs {
                hasher.update(ip.to_string().as_bytes());
                hasher.update([prefix]);
            }
        }
        Ok(hasher.finalize().to_vec())
    }

    /// Checks once. Returns whether a native network change was detected.
    ///
    /// The first call only records the checksum, like Go.
    pub fn check_once(&self) -> Result<bool, String> {
        self.check_once_inner(true)
    }

    /// Checks once after a manual trigger: the online status is not
    /// investigated, like the Go worker.
    pub fn check_once_triggered(&self) -> Result<bool, String> {
        self.check_once_inner(false)
    }

    fn check_once_inner(&self, investigate_online: bool) -> Result<bool, String> {
        let checksum = self.checksum()?;
        let mut last = self
            .last_checksum
            .lock()
            .unwrap_or_else(|error| error.into_inner());

        match last.as_ref() {
            None => {
                *last = Some(checksum);
                Ok(false)
            }
            Some(previous) if *previous == checksum => Ok(false),
            Some(_) => {
                *last = Some(checksum);
                notify_network_change();
                if investigate_online {
                    if let Some(trigger) = &self.online_trigger {
                        trigger();
                    }
                }
                Ok(true)
            }
        }
    }

    /// Runs the monitor until the token is cancelled.
    pub async fn run(self: &Arc<Self>, token: CancellationToken) {
        loop {
            let interval = if self.netenv.online() {
                ONLINE_CHECK_INTERVAL
            } else {
                OFFLINE_CHECK_INTERVAL
            };

            tokio::select! {
                () = token.cancelled() => return,
                () = self.trigger.notified() => {
                    // A manual trigger does not run the online check, like Go.
                    let _ = self.check_once_triggered();
                }
                () = tokio::time::sleep(interval) => {
                    if let Err(error) = self.check_once() {
                        tracing::warn!("netenv: failed to check for network changes: {error}");
                    }
                }
            }
        }
    }
}
