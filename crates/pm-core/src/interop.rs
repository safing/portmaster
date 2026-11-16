//! Wires the interoperability crate to the core's services.
//!
//! All privileged or machine-specific parts stay in the core: configuration,
//! the notification registry, the device location flag, the `core` database
//! and the interception state. `pm-interop` itself needs no root privileges.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use pm_config::Config;
use pm_database::{Controller, Databases, Record};
use pm_interop::InteropEnv;
use pm_notifications::Notifications;
use tokio::sync::broadcast;

use crate::location::CoreDeviceLocations;

/// Environment backed by the live core services.
pub struct CoreInteropEnv {
    config: Arc<Config>,
    notifications: Arc<Notifications>,
    device_locations: Arc<CoreDeviceLocations>,
    db: Arc<Controller>,
    verdict_enabled: Arc<AtomicBool>,
    interception_active: Arc<AtomicBool>,
    interception_tx: broadcast::Sender<()>,
    version: String,
}

impl CoreInteropEnv {
    /// Creates the environment.
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        config: Arc<Config>,
        databases: &Databases,
        notifications: Arc<Notifications>,
        device_locations: Arc<CoreDeviceLocations>,
        verdict_enabled: Arc<AtomicBool>,
        version: String,
    ) -> anyhow::Result<CoreInteropEnv> {
        let db = databases.controller("core")?;
        let interception_active = Arc::new(AtomicBool::new(
            config.get_as_bool("filter/enable", true)(),
        ));
        let (interception_tx, _) = broadcast::channel(16);
        Ok(CoreInteropEnv {
            config,
            notifications,
            device_locations,
            db,
            verdict_enabled,
            interception_active,
            interception_tx,
            version,
        })
    }

    /// Publishes the current interception state and notifies the IVPN engine.
    pub fn set_interception_started(&self, started: bool) {
        self.interception_active.store(started, Ordering::SeqCst);
        let _ = self.interception_tx.send(());
    }

    /// Returns the shared flag that enables the external verdict handler.
    pub fn verdict_enabled(&self) -> Arc<AtomicBool> {
        Arc::clone(&self.verdict_enabled)
    }

    /// Database-local part of the suppression key, derived from the Go key so
    /// the two cannot drift apart.
    fn suppression_key(&self) -> &str {
        pm_interop::NOTIFICATION_DB_ID_IVPN_DETECT_SUPPRESSED
            .strip_prefix("core:")
            .unwrap_or(pm_interop::NOTIFICATION_DB_ID_IVPN_DETECT_SUPPRESSED)
    }
}

impl InteropEnv for CoreInteropEnv {
    fn dns_listen_address(&self) -> String {
        // Go splits host and port with `net.SplitHostPort`, then falls back to
        // loopback when the host is missing, unparsable or `0.0.0.0`.
        let configured = self.config.get_as_string("dns/listenAddress", "")();
        let host = configured
            .parse::<std::net::SocketAddr>()
            .map(|address| address.ip().to_string())
            .unwrap_or(configured);
        match host.parse::<std::net::IpAddr>() {
            Ok(ip) if !ip.is_unspecified() => ip.to_string(),
            _ => "127.0.0.1".to_string(),
        }
    }

    fn dns_name_servers(&self) -> Vec<String> {
        self.config.get_as_string_array("dns/nameservers", &[])()
    }

    fn interception_started(&self) -> bool {
        self.interception_active.load(Ordering::SeqCst)
    }

    fn subscribe_interception(&self) -> broadcast::Receiver<()> {
        self.interception_tx.subscribe()
    }

    fn subscribe_config_change(&self) -> broadcast::Receiver<()> {
        self.config.subscribe_changes()
    }

    fn ensure_verdict_handler_registered(&self) {
        self.verdict_enabled.store(true, Ordering::SeqCst);
    }

    fn set_network_derived_location_disabled(&self, disable: bool) {
        self.device_locations.disable_network_derived_location(disable);
    }

    fn is_notification_suppressed(&self) -> bool {
        self.db.get(self.suppression_key()).is_ok()
    }

    fn suppress_notification(&self) -> Result<(), String> {
        let record = Record {
            key: pm_database::Key::new("core", self.suppression_key()),
            meta: pm_database::Meta::default(),
            format: pm_dsd::format::JSON,
            data: b"{}".to_vec(),
        };
        self.db.put(record).map_err(|error| error.to_string())
    }

    fn notifications(&self) -> &Arc<Notifications> {
        &self.notifications
    }

    fn version(&self) -> String {
        self.version.clone()
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn suppression_key_keeps_the_go_prefix() {
        assert_eq!(
            pm_interop::NOTIFICATION_DB_ID_IVPN_DETECT_SUPPRESSED,
            "core:notifications/interop/ivpn/suppressed"
        );
    }
}
