//! DNS stack: the resolver and the local nameserver.

use std::net::{IpAddr, SocketAddr};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, OnceLock, RwLock};
use std::time::Duration;

use async_trait::async_trait;
use pm_api::Api;
use pm_config::Config;
use pm_database::Databases;
use pm_firewall::dns_filter::FirewallDnsHook;
use pm_firewall::packet::{LayeredProfileLookup, PacketEnv};
use pm_nameserver::{
    DnsRequestInfo, FilterOutcome, FirewallHook, ListenerEvent, Nameserver, NameserverDeps,
    NameserverIpMatcher, NetstateLookup, PermissiveHook, RequestAttributor, Running, ServerEnv,
    StartOptions, SystemEnv,
};
use pm_netenv::{network_changed_flag, OnlineStatus};
use pm_netstate::{NetState, ProcFs};
use pm_network::{Connection, ConnectionType, Network, SharedConnection};
use pm_notifications::{
    Action, ActionTypeOpenSettingPayload, Notification, Notifications, ACTION_TYPE_OPEN_SETTING,
    ACTION_TYPE_OPEN_URL,
};
use pm_process::ProcReader;
use pm_resolver::env::{Environment, Nameserver as AssignedNameserver};
use pm_resolver::{
    ConfigSettings, IpInfoStore, NameRecordStore, Query, ResolverService, RRCache, Settings,
    SystemEnvironment,
};
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

    /// Feed the resolver's connectivity result back into netenv, so the online
    /// monitor can short-circuit to Online when a configured resolver answers.
    fn set_connected_to_dns(&self, connected: bool) {
        pm_netenv::set_connected_to_dns(connected);
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

/// Returned when a firewall hook is installed twice on the DNS stack.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FirewallHookAlreadySet;

impl std::fmt::Display for FirewallHookAlreadySet {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("firewall hook already set")
    }
}

impl std::error::Error for FirewallHookAlreadySet {}

/// Forwards firewall steps to the real hook once the core has built it. The
/// resolver and nameserver start before the firewall, so until then requests
/// are allowed through, as they were with the old `PermissiveHook`.
struct DeferredHook {
    hook: Arc<OnceLock<Arc<dyn FirewallHook>>>,
    fallback: PermissiveHook,
}

#[async_trait]
impl FirewallHook for DeferredHook {
    async fn filter_connection(&self, conn: &SharedConnection) -> FilterOutcome {
        match self.hook.get() {
            Some(hook) => hook.filter_connection(conn).await,
            None => self.fallback.filter_connection(conn).await,
        }
    }

    async fn filter_resolved_dns(
        &self,
        conn: &SharedConnection,
        q: &Query,
        rr_cache: RRCache,
    ) -> (Option<RRCache>, FilterOutcome) {
        match self.hook.get() {
            Some(hook) => hook.filter_resolved_dns(conn, q, rr_cache).await,
            None => self.fallback.filter_resolved_dns(conn, q, rr_cache).await,
        }
    }

    fn update_ips_and_cnames(&self, q: &Query, rr: &RRCache, conn: &SharedConnection) {
        match self.hook.get() {
            Some(hook) => hook.update_ips_and_cnames(q, rr, conn),
            None => self.fallback.update_ips_and_cnames(q, rr, conn),
        }
    }

    fn save_open_dns_request(&self, q: &Query, rr: &RRCache, conn: &SharedConnection) {
        match self.hook.get() {
            Some(hook) => hook.save_open_dns_request(q, rr, conn),
            None => self.fallback.save_open_dns_request(q, rr, conn),
        }
    }

    fn save_connection(&self, conn: &SharedConnection) {
        match self.hook.get() {
            Some(hook) => hook.save_connection(conn),
            None => self.fallback.save_connection(conn),
        }
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
type MatcherSink = Arc<dyn Fn(NameserverIpMatcher) + Send + Sync>;

pub struct Dns {
    resolver: ResolverService,
    running: Arc<tokio::sync::Mutex<Option<Running>>>,
    /// The current nameserver matcher, refreshed whenever the listeners start.
    matcher: Arc<RwLock<Option<NameserverIpMatcher>>>,
    /// Set by the core once the firewall is available.
    firewall_hook: Arc<OnceLock<Arc<dyn FirewallHook>>>,
    /// Called whenever the nameserver matcher changes, so the firewall can
    /// replace its old matcher instead of keeping a stale one.
    matcher_sink: Arc<RwLock<Option<MatcherSink>>>,
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
        let system_env_concrete = Arc::new(SystemEnv::new(network_service));
        let system_env: Arc<dyn ServerEnv> = Arc::clone(&system_env_concrete) as Arc<dyn ServerEnv>;
        let firewall_hook = Arc::new(OnceLock::new());
        let matcher = Arc::new(RwLock::new(None));
        let matcher_sink: Arc<RwLock<Option<MatcherSink>>> = Arc::new(RwLock::new(None));
        let listeners = Arc::new(Listeners {
            nameserver: Arc::new(Nameserver::new(NameserverDeps {
                resolver: Arc::new(resolver.clone()),
                attributor: Arc::new(BasicAttributor),
                hook: Arc::new(DeferredHook {
                    hook: Arc::clone(&firewall_hook),
                    fallback: PermissiveHook::default(),
                }),
                env: Arc::clone(&system_env),
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
        {
            let guard = running.lock().await;
            if let Some(running) = guard.as_ref() {
                *matcher.write().unwrap_or_else(|error| error.into_inner()) =
                    Some(running.ip_matcher());
            }
        }

        let cancel = CancellationToken::new();
        let mut tasks = Vec::new();

        // Reset failing queries after a network change, like Go's nameserver
        // reading `netenv.GetNetworkChangedFlag`.
        tasks.push(tokio::spawn({
            let system_env = Arc::clone(&system_env_concrete);
            let resolver_for_change = resolver.clone();
            let cancel = cancel.clone();
            let flag = network_changed_flag();
            async move {
                let mut ticker = tokio::time::interval(Duration::from_secs(1));
                loop {
                    tokio::select! {
                        () = cancel.cancelled() => return,
                        _ = ticker.tick() => {
                            if flag.is_set() {
                                system_env.set_network_changed();
                                reload(&resolver_for_change).await;
                                flag.refresh();
                            }
                        }
                    }
                }
            }
        }));

        // Reload the resolvers and restart the listeners on config changes.
        let mut previous_servers = settings.nameservers().join(" ");
        tasks.push(tokio::spawn({
            let cancel = cancel.clone();
            let resolver = resolver.clone();
            let running = Arc::clone(&running);
            let matcher = Arc::clone(&matcher);
            let matcher_sink: Arc<RwLock<Option<MatcherSink>>> = Arc::clone(&matcher_sink);
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
                        let started = listeners.start(&address).await;
                        if let Some(running) = started.as_ref() {
                            let updated = running.ip_matcher();
                            *matcher.write().unwrap_or_else(|error| error.into_inner()) =
                                Some(updated.clone());
                            if let Some(sink) = matcher_sink
                                .read()
                                .expect("matcher_sink lock poisoned")
                                .as_ref()
                            {
                                sink(updated);
                            }
                        }
                        *running = started;
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
            matcher,
            firewall_hook,
            matcher_sink,
            cancel,
            tasks: Mutex::new(tasks),
        })
    }

    /// The resolver.
    pub fn resolver(&self) -> &ResolverService {
        &self.resolver
    }

    /// Installs the firewall hook used by every nameserver request. The core
    /// calls this after the firewall has been built, because the resolver must
    /// start first. Only the first call takes effect.
    pub fn set_firewall_hook(
        &self,
        hook: Arc<dyn FirewallHook>,
    ) -> Result<(), FirewallHookAlreadySet> {
        self.firewall_hook
            .set(hook)
            .map_err(|_| FirewallHookAlreadySet)
    }

    /// Convenience wrapper: builds [`FirewallDnsHook`] from the firewall's
    /// shared pieces and installs it as the DNS firewall step.
    pub fn set_firewall_dns_hook(
        &self,
        env: Arc<dyn PacketEnv>,
        profiles: Arc<dyn LayeredProfileLookup>,
        ip_info: Arc<IpInfoStore>,
        network: Option<Arc<Network>>,
    ) -> Result<(), FirewallHookAlreadySet> {
        self.set_firewall_hook(Arc::new(FirewallDnsHook::new(
            env, profiles, ip_info, network,
        )))
    }

    /// The matcher for the local nameserver, to be installed on the firewall's
    /// packet handler with `set_nameserver_ip_matcher`. Refreshed whenever the
    /// listeners (re)start.
    pub fn nameserver_ip_matcher(&self) -> Option<NameserverIpMatcher> {
        self.matcher
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .clone()
    }

    /// Installs a callback that receives updated nameserver matchers.
    pub fn set_matcher_sink(&self, sink: MatcherSink) {
        *self
            .matcher_sink
            .write()
            .unwrap_or_else(|error| error.into_inner()) = Some(sink);
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
