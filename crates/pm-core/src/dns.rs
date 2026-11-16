//! DNS stack: the resolver and the local nameserver.

use std::net::{IpAddr, SocketAddr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use pm_api::Api;
use pm_config::Config;
use pm_database::Databases;
use pm_nameserver::{
    DnsRequestInfo, ListenerEvent, Nameserver, NameserverDeps, NetstateLookup, PermissiveHook,
    RequestAttributor, Running, StartOptions, SystemEnv,
};
use pm_netenv::OnlineStatus;
use pm_netstate::{NetState, ProcFs};
use pm_network::{Connection, ConnectionType, SharedConnection};
use pm_notifications::{
    Action, ActionTypeOpenSettingPayload, Notification, Notifications, ACTION_TYPE_OPEN_SETTING,
    ACTION_TYPE_OPEN_URL,
};
use pm_process::ProcReader;
use pm_resolver::env::{Environment, Nameserver as AssignedNameserver};
use pm_resolver::{ConfigSettings, NameRecordStore, ResolverService, Settings, SystemEnvironment};
use tokio::task::JoinHandle;
use tokio_util::sync::CancellationToken;

/// Option of the network service mode (Go `core/networkService`).
pub const CFG_NETWORK_SERVICE: &str = "core/networkService";

const FAILING_RESOLVERS_ID: &str = "resolver:all-configured-resolvers-failed";

const FAILING_RESOLVERS_MESSAGE: &str = "All configured DNS servers in Portmaster are failing.

You might not be able to connect to these servers, or all of these servers are offline.  
Choosing different DNS servers might fix this problem.

While the issue persists, Portmaster will use the DNS servers from your system or network, if permitted by configuration.

Alternatively, there might be something on your device that is interfering with Portmaster. This could be a firewall or another secure DNS resolver software. If that is your suspicion, please [check if you are running incompatible software here](https://docs.safing.io/portmaster/install/status/software-compatibility).

This notification will go away when Portmaster detects a working configured DNS server.";

/// Registers the options of the resolver and the nameserver. Call it before
/// the config is started.
pub fn register_options(config: &Config, nameserver_address: &str) -> anyhow::Result<()> {
    pm_resolver::settings::register_options(config)?;
    pm_nameserver::register_options(config, nameserver_address)?;
    Ok(())
}

/// The system environment, plus the notification about failing resolvers.
struct CoreEnvironment {
    system: SystemEnvironment,
    notifications: Arc<Notifications>,
    /// Whether the failing-resolvers notification is shown; it is shown once.
    notified: AtomicBool,
}

impl Environment for CoreEnvironment {
    fn online_status(&self) -> OnlineStatus {
        self.system.online_status()
    }

    fn is_connectivity_domain(&self, fqdn: &str) -> bool {
        self.system.is_connectivity_domain(fqdn)
    }

    fn nameservers(&self) -> Vec<AssignedNameserver> {
        self.system.nameservers()
    }

    fn is_local_network(&self, ip: IpAddr) -> bool {
        self.system.is_local_network(ip)
    }

    fn failing_resolvers_changed(&self, all_failing: bool) {
        if !all_failing {
            if self.notified.swap(false, Ordering::SeqCst) {
                self.notifications.delete(FAILING_RESOLVERS_ID);
            }
            return;
        }
        if self.notified.swap(true, Ordering::SeqCst) {
            return;
        }
        let payload = ActionTypeOpenSettingPayload {
            key: pm_resolver::settings::CFG_OPTION_NAME_SERVERS_KEY.to_string(),
            profile: String::new(),
        };
        self.notifications.notify(
            Notification::error(
                FAILING_RESOLVERS_ID,
                "Configured DNS Servers Failing",
                FAILING_RESOLVERS_MESSAGE,
            )
            .with_action(
                Action::new("", "Change DNS Servers").with_type(
                    ACTION_TYPE_OPEN_SETTING,
                    serde_json::to_value(payload).unwrap_or_default(),
                ),
            ),
        );
    }
}

/// Attributes requests without a process: the connection store is not wired
/// into the core yet.
struct BasicAttributor;

#[async_trait]
impl RequestAttributor for BasicAttributor {
    async fn attribute(&self, request: &DnsRequestInfo) -> Result<SharedConnection, String> {
        let conn = Connection {
            id: request.conn_id.clone(),
            conn_type: ConnectionType::Dns,
            external: !request.local,
            scope: request.fqdn.clone(),
            ..Connection::default()
        };
        Ok(Arc::new(Mutex::new(conn)))
    }
}

struct Listeners {
    nameserver: Arc<Nameserver>,
    notifications: Arc<Notifications>,
    lookup: Arc<NetstateLookup>,
}

impl Listeners {
    async fn start(&self, address: &str) -> Option<Running> {
        let notifications = Arc::clone(&self.notifications);
        let options = StartOptions {
            lookup: Some(self.lookup.clone()),
            on_event: Some(Arc::new(move |event| match event {
                ListenerEvent::Started { primary } => {
                    let suffix = if primary { "" } else { "-secondary" };
                    for id in [
                        "nameserver:conflicting-service",
                        "nameserver:listener-failed",
                    ] {
                        notifications.delete(&format!("{id}{suffix}"));
                    }
                }
                ListenerEvent::Failed(failure) => {
                    let mut notification =
                        Notification::error(&failure.event_id(), failure.title(), &failure.message());
                    if failure.is_conflict() {
                        notification = notification.with_action(
                            Action::new("", "Open Docs").with_type(
                                ACTION_TYPE_OPEN_URL,
                                serde_json::json!(pm_nameserver::COMPATIBILITY_DOCS_URL),
                            ),
                        );
                    }
                    notifications.notify(notification);
                }
            })),
        };
        match pm_nameserver::start(Arc::clone(&self.nameserver), address, options).await {
            Ok(running) => Some(running),
            Err(error) => {
                tracing::error!(%error, "nameserver: invalid listen address");
                self.notifications.notify(Notification::error(
                    "nameserver:listener-failed",
                    "Secure DNS Error",
                    &format!(
                        "The internal DNS server failed. Restart Portmaster to try again. Error: {error}"
                    ),
                ));
                None
            }
        }
    }
}

/// The running DNS stack.
pub struct Dns {
    resolver: ResolverService,
    running: Arc<tokio::sync::Mutex<Option<Running>>>,
    cancel: CancellationToken,
    tasks: Mutex<Vec<JoinHandle<()>>>,
}

impl Dns {
    /// Starts the resolver and the nameserver. Failing to listen is reported
    /// as a notification and does not fail the start.
    pub async fn start(
        config: &Arc<Config>,
        databases: &Arc<Databases>,
        notifications: &Arc<Notifications>,
        api: &Api,
        default_address: &str,
    ) -> anyhow::Result<Dns> {
        let mut changes = config.subscribe_changes();

        let store = NameRecordStore::new(
            databases.interface(pm_resolver::namerecord::record_interface_options()),
        );
        let settings = Arc::new(ConfigSettings::new(Arc::clone(config)));
        let environment = Arc::new(CoreEnvironment {
            system: SystemEnvironment,
            notifications: Arc::clone(notifications),
            notified: AtomicBool::new(false),
        });
        let resolver = ResolverService::new(settings.clone(), environment, store);
        reload(&resolver).await;
        pm_resolver::api::register_api(api, &resolver)?;

        let network_service = {
            let config = Arc::clone(config);
            move || config.get_as_bool(CFG_NETWORK_SERVICE, false)()
        };
        let listeners = Arc::new(Listeners {
            nameserver: Arc::new(Nameserver::new(NameserverDeps {
                resolver: Arc::new(resolver.clone()),
                attributor: Arc::new(BasicAttributor),
                hook: Arc::new(PermissiveHook::default()),
                env: Arc::new(SystemEnv::new(network_service)),
            })),
            notifications: Arc::clone(notifications),
            lookup: Arc::new(NetstateLookup {
                state: Arc::new(NetState::new(ProcFs::default())),
                procs: Arc::new(ProcReader::new(ProcFs::default())),
            }),
        });

        let address_key = pm_nameserver::CFG_DEFAULT_NAMESERVER_ADDRESS_KEY;
        let mut address = config.get_as_string(address_key, default_address)();
        let running = Arc::new(tokio::sync::Mutex::new(listeners.start(&address).await));

        let cancel = CancellationToken::new();
        let mut tasks = Vec::new();

        // Reload the resolvers and restart the listeners on config changes.
        let mut previous_servers = settings.nameservers().join(" ");
        tasks.push(tokio::spawn({
            let cancel = cancel.clone();
            let resolver = resolver.clone();
            let running = Arc::clone(&running);
            let config = Arc::clone(config);
            let default_address = default_address.to_string();
            async move {
                let get_address = config.get_as_string(address_key, &default_address);
                loop {
                    let received = tokio::select! {
                        () = cancel.cancelled() => return,
                        received = changes.recv() => received,
                    };
                    if let Err(tokio::sync::broadcast::error::RecvError::Closed) = received {
                        return;
                    }
                    let servers = settings.nameservers().join(" ");
                    if servers != previous_servers {
                        previous_servers = servers;
                        reload(&resolver).await;
                        tracing::debug!("resolver: reloaded nameservers due to config change");
                    }
                    let new_address = get_address();
                    if new_address != address {
                        address = new_address;
                        let mut running = running.lock().await;
                        if let Some(old) = running.take() {
                            old.shutdown().await;
                        }
                        *running = listeners.start(&address).await;
                    }
                }
            }
        }));

        // Check failing resolvers regularly.
        tasks.push(tokio::spawn({
            let resolver = resolver.clone();
            let cancel = cancel.clone();
            async move {
                loop {
                    if resolver.check_failing_resolvers(&cancel).await.is_err() {
                        return;
                    }
                    tokio::select! {
                        () = tokio::time::sleep(resolver.failing_resolver_check_interval()) => {}
                        () = cancel.cancelled() => return,
                    }
                }
            }
        }));

        Ok(Dns {
            resolver,
            running,
            cancel,
            tasks: Mutex::new(tasks),
        })
    }

    /// The resolver.
    pub fn resolver(&self) -> &ResolverService {
        &self.resolver
    }

    /// The addresses the nameserver listens on.
    pub async fn listen_addrs(&self) -> Vec<SocketAddr> {
        match self.running.lock().await.as_ref() {
            Some(running) => running.local_addrs(),
            None => Vec::new(),
        }
    }

    /// Stops the workers, the listeners and the resolver.
    pub async fn shutdown(&self) {
        self.cancel.cancel();
        let tasks = std::mem::take(&mut *self.tasks.lock().expect("lock poisoned"));
        for task in tasks {
            let _ = task.await;
        }
        if let Some(running) = self.running.lock().await.take() {
            running.shutdown().await;
        }
        self.resolver.shutdown();
    }

    /// Signals everything to stop without waiting; dropping the listeners
    /// cancels them.
    pub fn stop(&self) {
        self.cancel.cancel();
        if let Ok(mut running) = self.running.try_lock() {
            running.take();
        }
        self.resolver.shutdown();
    }
}

/// Loads the resolvers off the async workers: it reads `resolv.conf`.
async fn reload(resolver: &ResolverService) {
    let resolver = resolver.clone();
    if let Err(error) = tokio::task::spawn_blocking(move || resolver.load_resolvers()).await {
        tracing::error!(%error, "resolver: failed to load resolvers");
    }
}
