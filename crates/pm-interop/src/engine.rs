//! The IVPN interoperability engine: connection lifecycle, message handlers,
//! DNS coordination, notifications and the firewall verdict hook.

use std::net::IpAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex as StdMutex, RwLock};
use std::time::{SystemTime, UNIX_EPOCH};

use pm_network::Connection;
use pm_notifications::NotificationHandle;
use tokio::sync::{broadcast, Notify};
use tokio_util::sync::CancellationToken;

use crate::client::{ClientEvent, IvpnClient, IvpnClientFactory};
use crate::env::InteropEnv;
use crate::notifications::{
    attach_suppress_action, build_ivpn_notification, build_old_version_notification,
    EVENT_ID_IVPN, OLD_VERSION_EXPIRY_SECS,
};
use crate::protocol::{
    ip_protocol_name, parse_connected_resp, parse_connection_starting, ConnectedResp, DnsServerConfig,
    DnsSettings, MSG_CONNECTION_STARTING, MSG_CONNECTION_STOPPED, MSG_CONNECTED_RESP,
};
use crate::status::{decide, ClientStatus, ConnView, VerdictDecision, VpnConnectionInfo};

/// Description sent to the IVPN daemon when Portmaster overrides its DNS.
/// Kept identical to the Go `DNS_LOCKED_DESCRIPTION` constant.
pub const DNS_LOCKED_DESCRIPTION: &str = "Portmaster controls all DNS resolution on this system, which prevents conflicts with IVPN's DNS settings. To let IVPN manage DNS again, remove all DNS servers from Portmaster's DNS configuration.";

/// Deletes the "IVPN Client detected" notification when a connection attempt
/// ends, mirroring Go's deferred `notification.Delete()`.
struct NotificationGuard {
    handle: Arc<NotificationHandle>,
    notifications: Arc<pm_notifications::Notifications>,
}

impl Drop for NotificationGuard {
    fn drop(&mut self) {
        self.notifications.delete_handle(&self.handle, true);
    }
}

/// Aborts the background event pump when a connection attempt ends.
struct PumpGuard {
    cancel: CancellationToken,
    task: tokio::task::JoinHandle<()>,
}

impl Drop for PumpGuard {
    fn drop(&mut self) {
        self.cancel.cancel();
        self.task.abort();
    }
}

/// Port of Go `InteropIvpn`.
pub struct InteropIvpn {
    env: Arc<dyn InteropEnv>,
    factory: Arc<dyn IvpnClientFactory>,
    status: RwLock<Option<ClientStatus>>,
    is_custom_dns_active: AtomicBool,
    notif_warn_old_version: StdMutex<Option<Arc<NotificationHandle>>>,
    first_try_done: Notify,
    active_worker: StdMutex<Option<CancellationToken>>,
    client_disconnected: Notify,
}

impl InteropIvpn {
    /// Creates the engine with injectable environment and client factory.
    pub fn new(env: Arc<dyn InteropEnv>, factory: Arc<dyn IvpnClientFactory>) -> InteropIvpn {
        InteropIvpn {
            env,
            factory,
            status: RwLock::new(None),
            is_custom_dns_active: AtomicBool::new(false),
            notif_warn_old_version: StdMutex::new(None),
            first_try_done: Notify::new(),
            active_worker: StdMutex::new(None),
            client_disconnected: Notify::new(),
        }
    }

    /// Returns a copy of the current status, or an empty one like Go
    /// `getStatus`.
    pub fn status_snapshot(&self) -> ClientStatus {
        self.status
            .read()
            .expect("lock poisoned")
            .clone()
            .unwrap_or_default()
    }

    /// Overwrites the status, like Go `setStatus`.
    fn set_status(&self, status: ClientStatus) {
        *self.status.write().expect("lock poisoned") = Some(status);
    }

    /// Starts the engine and waits for the first connection attempt, like Go
    /// `Start` waiting on `firstTryChan`.
    pub async fn start(self: &Arc<Self>) {
        self.spawn_worker();
        self.first_try_done.notified().await;
    }

    /// Handles a ping by starting a fresh connection attempt, like Go
    /// `PingHandler`.
    pub fn ping_handler(self: &Arc<Self>) {
        self.spawn_worker();
    }

    fn spawn_worker(self: &Arc<Self>) {
        if let Some(old) = self.active_worker.lock().expect("lock poisoned").take() {
            old.cancel();
        }
        let cancel = CancellationToken::new();
        *self.active_worker.lock().expect("lock poisoned") = Some(cancel.clone());
        let this = Arc::clone(self);
        tokio::spawn(async move {
            this.connect_worker(cancel).await;
        });
    }

    /// Computes a firewall verdict, like Go `VerdictHandler`.
    pub fn verdict_handler(&self, conn: &Connection) -> VerdictDecision {
        let status = self.status.read().expect("lock poisoned");
        decide(status.as_ref(), &ConnView::from(conn))
    }

    async fn connect_worker(self: Arc<Self>, cancel: CancellationToken) {
        let result = self.connect_inner(&cancel).await;

        // Go's deferred cleanup: re-enable network-derived locations, clear the
        // status, reset DNS tracking, mark the first attempt done and reconcile
        // compatibility rules.
        self.env.set_network_derived_location_disabled(false);
        *self.status.write().expect("lock poisoned") = None;
        self.is_custom_dns_active.store(false, Ordering::SeqCst);
        self.first_try_done.notify_one();
        self.reconcile_compatibility_state();

        if let Err(error) = result {
            tracing::warn!("ivpn: connection attempt failed: {error}");
        }
    }

    async fn connect_inner(self: &Arc<Self>, cancel: &CancellationToken) -> Result<(), String> {
        // Delete a previously shown old-version warning at the start of every
        // attempt, like the Go code does with `notifWarnOldVersion`.
        if let Some(old) = self.notif_warn_old_version.lock().expect("lock poisoned").take() {
            self.env.notifications().delete_handle(&old, true);
        }

        let (service_port, _) = self.factory.connection_port_info()?;
        let mut status = self.status_snapshot();
        status.service_port = service_port;
        self.set_status(status);

        // Knowing the service port lets the verdict handler accept connections
        // to the daemon while the client is still connecting.
        self.env.ensure_verdict_handler_registered();

        let client = self.factory.create_client();
        // Subscribe before connecting so messages emitted during the handshake
        // are not lost, like Go registering handlers before `Connect`.
        let events = client.subscribe();
        if client.connect().await.is_err() {
            // Expected when the IVPN client is not running.
            return Ok(());
        }

        let pump_cancel = CancellationToken::new();
        let pump_this = Arc::clone(self);
        let mut pump_events = events;
        let pump_task_cancel = pump_cancel.clone();
        let pump_task = tokio::spawn(async move {
            loop {
                tokio::select! {
                    () = pump_task_cancel.cancelled() => break,
                    event = pump_events.recv() => {
                        match event {
                            Ok(ClientEvent::Message { name, data }) => {
                                pump_this.dispatch_message(&name, &data);
                            }
                            Ok(ClientEvent::Disconnected) => {
                                pump_this.client_disconnected.notify_one();
                                break;
                            }
                            Err(broadcast::error::RecvError::Lagged(_)) => continue,
                            Err(broadcast::error::RecvError::Closed) => {
                                pump_this.client_disconnected.notify_one();
                                break;
                            }
                        }
                    }
                }
            }
        });
        let _pump_guard = PumpGuard {
            cancel: pump_cancel,
            task: pump_task,
        };

        let hello = client.hello(&self.env.version(), "Portmaster").await?;
        if hello.service_binary.is_empty() {
            // Version > v3.15.1 must report the service binary path.
            tracing::warn!(
                "Detected IVPN Client version '{}' is incompatible. The hello response did not include all required fields.",
                hello.version
            );
            let notification = self
                .env
                .notifications()
                .notify(build_old_version_notification(now_unix() + OLD_VERSION_EXPIRY_SECS));
            *self.notif_warn_old_version.lock().expect("lock poisoned") = notification;
            return Ok(());
        }

        let mut status = self.status_snapshot();
        status.service_binary = hello.service_binary;
        self.set_status(status);
        self.first_try_done.notify_one();
        tracing::debug!("Connected to IVPN client {}", hello.version);

        // Show the UI notification unless the user suppressed it.
        let _notification_guard = if !self.env.is_notification_suppressed() {
            if let Some(handle) = self
                .env
                .notifications()
                .notify(build_ivpn_notification())
            {
                let env = Arc::clone(&self.env);
                attach_suppress_action(
                    Arc::clone(self.env.notifications()),
                    EVENT_ID_IVPN,
                    move || env.suppress_notification(),
                );
                Some(NotificationGuard {
                    handle,
                    notifications: Arc::clone(self.env.notifications()),
                })
            } else {
                None
            }
        } else {
            None
        };

        // Apply the DNS settings derived from the current configuration.
        self.update_ivpn_client_dns_settings(client.as_ref()).await;

        let mut interception_events = self.env.subscribe_interception();
        let mut config_events = self.env.subscribe_config_change();
        loop {
            tokio::select! {
                () = cancel.cancelled() => {
                    client.disconnect();
                    break;
                }
                () = self.client_disconnected.notified() => break,
                _ = interception_events.recv() => {
                    self.update_ivpn_client_dns_settings(client.as_ref()).await;
                }
                _ = config_events.recv() => {
                    self.update_ivpn_client_dns_settings(client.as_ref()).await;
                    self.reconcile_compatibility_state();
                }
            }
        }

        tracing::debug!("IVPN client disconnected");
        Ok(())
    }

    fn dispatch_message(&self, name: &str, data: &str) {
        match name {
            MSG_CONNECTION_STARTING => self.on_connection_starting(data),
            MSG_CONNECTION_STOPPED => self.on_connection_stopped(),
            MSG_CONNECTED_RESP => self.on_connected_resp(data),
            _ => {}
        }
    }

    /// Handler for the `ConnectionStarting` daemon message.
    fn on_connection_starting(&self, message_data: &str) {
        // Virtual VPN interfaces can skew the detected location, so ignore
        // network-derived location sources while the VPN is active. Go does
        // this before parsing the message.
        self.env.set_network_derived_location_disabled(true);

        let conn_info = match parse_connection_starting(message_data) {
            Ok(info) => info,
            Err(error) => {
                tracing::warn!("IVPN: Failed to parse ConnectionStarting message: {error}");
                return;
            }
        };

        let dst_address = conn_info.address.parse::<IpAddr>().ok();
        let connection = VpnConnectionInfo {
            // Invalidate the port if the address is invalid, to avoid false
            // matches in firewall verdicts.
            dst_port: if dst_address.is_some() { conn_info.port } else { 0 },
            dst_address,
            protocol: conn_info.protocol,
        };

        let mut status = self.status_snapshot();
        status.vpn_connection = connection;
        self.set_status(status);

        tracing::debug!(
            "IVPN: VPN connection starting {}:{} {}",
            conn_info.address,
            conn_info.port,
            ip_protocol_name(conn_info.protocol)
        );
    }

    /// Handler for the `ConnectionStopped` daemon message.
    fn on_connection_stopped(&self) {
        self.env.set_network_derived_location_disabled(false);

        let mut status = self.status_snapshot();
        status.vpn_connection = VpnConnectionInfo::default();
        status.connected_info = None;
        self.set_status(status);

        tracing::debug!("IVPN: VPN connection stopped");
        self.reconcile_compatibility_state();
    }

    /// Handler for the `ConnectedResp` daemon message.
    fn on_connected_resp(&self, message_data: &str) {
        let connected_resp: ConnectedResp = match parse_connected_resp(message_data) {
            Ok(resp) => resp,
            Err(error) => {
                tracing::warn!("IVPN: Failed to parse ConnectedResp message: {error}");
                return;
            }
        };

        let mut status = self.status_snapshot();
        status.connected_info = Some(connected_resp.clone());
        self.set_status(status);

        tracing::debug!(
            "IVPN: VPN connection established (vpnType:{}; localIPv4:{}; localIPv6:{})",
            connected_resp.vpn_type_name(),
            connected_resp.client_ip,
            connected_resp.client_ipv6
        );
        self.reconcile_compatibility_state();
    }

    /// Reconciles SPN/split-tunnel compatibility rules with the current WireGuard
    /// state. The Go implementation (`hook_linux.go`) manipulates nftables and
    /// iptables and requires root; it is intentionally not ported.
    fn reconcile_compatibility_state(&self) {}

    /// Configures the IVPN client to use Portmaster's resolver when custom DNS
    /// is configured and interception is active.
    async fn update_ivpn_client_dns_settings(&self, client: &dyn IvpnClient) {
        let want_custom_dns =
            !self.env.dns_name_servers().is_empty() && self.env.interception_started();
        if want_custom_dns == self.is_custom_dns_active.load(Ordering::SeqCst) {
            return;
        }

        if want_custom_dns {
            let address = self.env.dns_listen_address();
            let dns = DnsSettings {
                servers: vec![DnsServerConfig {
                    address: address.clone(),
                    ..DnsServerConfig::default()
                }],
            };
            match client
                .set_temp_prioritized_dns(dns, DNS_LOCKED_DESCRIPTION)
                .await
            {
                Ok(()) => {
                    tracing::debug!("IVPN: Manual DNS set successfully to {address:?}");
                    self.is_custom_dns_active.store(true, Ordering::SeqCst);
                }
                Err(error) => {
                    tracing::warn!("IVPN: Failed to set manual DNS: {error}");
                }
            }
            return;
        }

        match client
            .set_temp_prioritized_dns(DnsSettings::default(), "")
            .await
        {
            Ok(()) => {
                self.is_custom_dns_active.store(false, Ordering::SeqCst);
                tracing::debug!("IVPN: Manual DNS restored successfully");
            }
            Err(error) => {
                tracing::warn!("IVPN: Failed to restore manual DNS: {error}");
            }
        }
    }
}

/// Current Unix time in seconds, like Go `time.Now().Unix()`.
fn now_unix() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::env::TestEnv;
    use crate::notifications::{EVENT_ID_IVPN_OLD_VERSION, NOTIFICATION_DB_ID_IVPN_DETECT_SUPPRESSED};
    use crate::protocol::{PROTOCOL_UDP, VPN_TYPE_WIREGUARD};
    use async_trait::async_trait;
    use std::sync::atomic::AtomicBool as StdAtomicBool;
    use std::sync::Mutex;

    #[derive(Default, Clone)]
    struct RecordedDns {
        description: String,
        settings: DnsSettings,
    }

    struct MockClient {
        events_tx: broadcast::Sender<ClientEvent>,
        hello: HelloOutcome,
        dns_calls: Mutex<Vec<RecordedDns>>,
        connected: StdAtomicBool,
    }

    enum HelloOutcome {
        Version(String),
        Incompatible(String),
        Error,
    }

    impl MockClient {
        fn new(hello: HelloOutcome) -> (Arc<MockClient>, broadcast::Sender<ClientEvent>) {
            let (events_tx, _) = broadcast::channel(16);
            let client = Arc::new(MockClient {
                events_tx: events_tx.clone(),
                hello,
                dns_calls: Mutex::new(Vec::new()),
                connected: StdAtomicBool::new(false),
            });
            (client, events_tx)
        }

        fn emit(&self, name: &str, data: &str) {
            let _ = self.events_tx.send(ClientEvent::Message {
                name: name.to_string(),
                data: data.to_string(),
            });
        }

        fn dns_calls(&self) -> Vec<RecordedDns> {
            self.dns_calls.lock().expect("lock poisoned").clone()
        }
    }

    #[async_trait]
    impl IvpnClient for MockClient {
        async fn connect(&self) -> Result<(), String> {
            self.connected.store(true, Ordering::SeqCst);
            Ok(())
        }

        async fn hello(&self, _version: &str, _name: &str) -> Result<crate::protocol::HelloResp, String> {
            match &self.hello {
                HelloOutcome::Version(version) => Ok(crate::protocol::HelloResp {
                    version: version.clone(),
                    service_binary: "/opt/ivpn/service".to_string(),
                    ..Default::default()
                }),
                HelloOutcome::Incompatible(version) => Ok(crate::protocol::HelloResp {
                    version: version.clone(),
                    ..Default::default()
                }),
                HelloOutcome::Error => Err("hello failed".to_string()),
            }
        }

        async fn set_temp_prioritized_dns(
            &self,
            dns: DnsSettings,
            description: &str,
        ) -> Result<(), String> {
            self.dns_calls.lock().expect("lock poisoned").push(RecordedDns {
                description: description.to_string(),
                settings: dns,
            });
            Ok(())
        }

        fn subscribe(&self) -> broadcast::Receiver<ClientEvent> {
            self.events_tx.subscribe()
        }

        fn disconnect(&self) {
            self.connected.store(false, Ordering::SeqCst);
            let _ = self.events_tx.send(ClientEvent::Disconnected);
        }
    }

    struct MockFactory {
        client: Arc<MockClient>,
        port: u16,
    }

    impl IvpnClientFactory for MockFactory {
        fn connection_port_info(&self) -> Result<(u16, u64), String> {
            Ok((self.port, 0))
        }

        fn create_client(&self) -> Arc<dyn IvpnClient> {
            self.client.clone()
        }
    }

    fn engine(env: Arc<TestEnv>, hello: HelloOutcome) -> (Arc<InteropIvpn>, Arc<MockClient>) {
        let (client, _) = MockClient::new(hello);
        let factory = Arc::new(MockFactory {
            client: Arc::clone(&client),
            port: 45000,
        });
        (Arc::new(InteropIvpn::new(env, factory)), client)
    }

    #[tokio::test]
    async fn start_populates_status_and_shows_notification() {
        let env = Arc::new(TestEnv::new());
        let (interop, client) = engine(Arc::clone(&env), HelloOutcome::Version("3.16.0".into()));
        interop.start().await;

        let status = interop.status_snapshot();
        assert_eq!(status.service_port, 45000);
        assert_eq!(status.service_binary, "/opt/ivpn/service");
        assert!(env.verdict_registered.load(Ordering::SeqCst));
        assert!(env.notifications.get(EVENT_ID_IVPN).is_some());
        assert!(client.connected.load(Ordering::SeqCst));
    }

    #[tokio::test]
    async fn incompatible_version_shows_warning_only() {
        let env = Arc::new(TestEnv::new());
        let (interop, _) = engine(Arc::clone(&env), HelloOutcome::Incompatible("3.15.1".into()));
        interop.start().await;

        assert!(env.notifications.get(EVENT_ID_IVPN_OLD_VERSION).is_some());
        assert!(env.notifications.get(EVENT_ID_IVPN).is_none());
        assert_eq!(interop.status_snapshot().service_binary, "");
    }

    #[tokio::test]
    async fn hello_error_returns_without_service_binary() {
        let env = Arc::new(TestEnv::new());
        let (interop, _) = engine(Arc::clone(&env), HelloOutcome::Error);
        interop.start().await;

        // The deferred cleanup clears the status on a failed attempt, just
        // like Go `setStatus(nil)`.
        let status = interop.status_snapshot();
        assert_eq!(status.service_port, 0);
        assert!(status.service_binary.is_empty());
        assert!(env.notifications.get(EVENT_ID_IVPN).is_none());
    }

    #[tokio::test]
    async fn suppressed_user_does_not_see_notification() {
        let env = Arc::new(TestEnv::new());
        env.suppressed.store(true, Ordering::SeqCst);
        let (interop, _) = engine(Arc::clone(&env), HelloOutcome::Version("3.16.0".into()));
        interop.start().await;

        assert!(env.notifications.get(EVENT_ID_IVPN).is_none());
    }

    #[tokio::test]
    async fn suppress_action_persists_choice() {
        let env = Arc::new(TestEnv::new());
        let (interop, _) = engine(Arc::clone(&env), HelloOutcome::Version("3.16.0".into()));
        interop.start().await;

        assert!(env.notifications.select_action(EVENT_ID_IVPN, "suppress"));
        assert!(env.suppressed.load(Ordering::SeqCst));
        assert_eq!(
            NOTIFICATION_DB_ID_IVPN_DETECT_SUPPRESSED,
            "core:notifications/interop/ivpn/suppressed"
        );
    }

    #[tokio::test]
    async fn event_handlers_update_status_and_location() {
        let env = Arc::new(TestEnv::new());
        let (interop, client) = engine(Arc::clone(&env), HelloOutcome::Version("3.16.0".into()));
        interop.start().await;

        client.emit(
            MSG_CONNECTION_STARTING,
            r#"{"Address":"203.0.113.7","Port":1194,"Protocol":17}"#,
        );
        // The event pump is asynchronous; wait for the status to be updated.
        tokio::time::timeout(std::time::Duration::from_secs(2), async {
            loop {
                if interop.status_snapshot().vpn_connection.is_valid() {
                    break;
                }
                tokio::task::yield_now().await;
            }
        })
        .await
        .expect("status updated");

        let status = interop.status_snapshot();
        assert_eq!(status.vpn_connection.dst_port, 1194);
        assert_eq!(status.vpn_connection.protocol, PROTOCOL_UDP);
        assert!(env.location_disabled.load(Ordering::SeqCst));

        client.emit(MSG_CONNECTED_RESP, r#"{"VpnType":1,"ClientIP":"10.50.0.2"}"#);
        tokio::time::timeout(std::time::Duration::from_secs(2), async {
            loop {
                if interop.status_snapshot().connected_info.is_some() {
                    break;
                }
                tokio::task::yield_now().await;
            }
        })
        .await
        .expect("connected info updated");
        let status = interop.status_snapshot();
        assert_eq!(
            status.connected_info.expect("connected").vpn_type,
            VPN_TYPE_WIREGUARD
        );

        client.emit(MSG_CONNECTION_STOPPED, "{}");
        tokio::time::timeout(std::time::Duration::from_secs(2), async {
            loop {
                if interop.status_snapshot().connected_info.is_none() {
                    break;
                }
                tokio::task::yield_now().await;
            }
        })
        .await
        .expect("stop handled");
        assert!(!env.location_disabled.load(Ordering::SeqCst));
    }

    #[tokio::test]
    async fn malformed_event_does_not_clear_status() {
        let env = Arc::new(TestEnv::new());
        let (interop, client) = engine(Arc::clone(&env), HelloOutcome::Version("3.16.0".into()));
        interop.start().await;
        client.emit(MSG_CONNECTION_STARTING, r#"{"Port":"bad"}"#);
        assert!(!interop.status_snapshot().vpn_connection.is_valid());
    }

    #[tokio::test]
    async fn dns_settings_follow_config_and_interception() {
        let env = Arc::new(
            TestEnv::new()
                .with_dns_servers(&["1.1.1.1"])
                .with_dns_listen("127.0.0.1")
                .with_interception(false),
        );
        let (interop, client) = engine(Arc::clone(&env), HelloOutcome::Version("3.16.0".into()));
        interop.start().await;
        assert!(client.dns_calls().is_empty());

        env.interception.store(true, Ordering::SeqCst);
        let _ = env.interception_tx.send(());
        tokio::time::timeout(std::time::Duration::from_secs(2), async {
            loop {
                if !client.dns_calls().is_empty() {
                    break;
                }
                tokio::task::yield_now().await;
            }
        })
        .await
        .expect("dns set");
        let calls = client.dns_calls();
        assert_eq!(calls[0].description, DNS_LOCKED_DESCRIPTION);
        assert_eq!(calls[0].settings.servers[0].address, "127.0.0.1");

        env.interception.store(false, Ordering::SeqCst);
        let _ = env.interception_tx.send(());
        tokio::time::timeout(std::time::Duration::from_secs(2), async {
            loop {
                if client.dns_calls().len() >= 2 {
                    break;
                }
                tokio::task::yield_now().await;
            }
        })
        .await
        .expect("dns restored");
        let calls = client.dns_calls();
        assert!(calls[1].settings.is_empty());
        assert!(calls[1].description.is_empty());
    }
}
