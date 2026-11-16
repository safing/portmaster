//! Live firewall wiring for the core (A7b-3).
//!
//! Builds the environment the firewall deciders need from the core's modules,
//! creates the connection store and packet handler, and (when the filter is
//! enabled and the process has the required privileges) installs iptables and
//! starts the nfqueue readers. Root-dependent parts are guarded so the core
//! still builds and tests unprivileged.

use std::net::IpAddr;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex, RwLock};

use pm_config::Config;
use pm_firewall::packet::{
    BandwidthSink, ConntrackControl, ExternalVerdict, LayeredProfileLookup, NfqPacket, PacketEnv,
    PacketHandler, SystemConntrack,
};
use pm_firewall::{
    CustomLists, FirewallEnv, LocalNetwork, MulticastRequest, ServiceCustomLists, SplitTunnelEnv,
};
use pm_splittun::{SplitTunnelEnvAdapter, SplitTunnelProfiles};
use pm_netenv::OnlineStatus;
use pm_network::{
    Environment, LocalProfileInfo, Network, ProcessResolver, ProfileLookup, SharedConnection,
    SystemProcessResolver,
};
use pm_process::{ProcReader, Process, ProcessCache};
use pm_profile::{GlobalProfileConfig, LayeredProfile, Profile, ProfileStore, SOURCE_LOCAL};
use pm_resolver::ipinfo::IpInfoStore;
use pm_resolver::ResolverService;
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;

/// Profiles of the local source and their layered views, keyed by process key.
pub struct CoreProfiles {
    config: Arc<Config>,
    global: Arc<GlobalProfileConfig>,
    store: ProfileStore,
    profiles: RwLock<Vec<Profile>>,
    layered: RwLock<std::collections::HashMap<String, Arc<LayeredProfile>>>,
}

impl CoreProfiles {
    /// Loads all local profiles from the `core` database.
    pub fn new(
        config: Arc<Config>,
        global: Arc<GlobalProfileConfig>,
        store: ProfileStore,
    ) -> Result<CoreProfiles, String> {
        let profiles = store.all(SOURCE_LOCAL).map_err(|error| error.to_string())?;
        Ok(CoreProfiles {
            config,
            global,
            store,
            profiles: RwLock::new(profiles),
            layered: RwLock::new(std::collections::HashMap::new()),
        })
    }

    /// Reloads the profiles and drops the layered views.
    pub fn refresh(&self) -> Result<(), String> {
        let profiles = self
            .store
            .all(SOURCE_LOCAL)
            .map_err(|error| error.to_string())?;
        *self.profiles.write().unwrap_or_else(|error| error.into_inner()) = profiles;
        self.layered
            .write()
            .unwrap_or_else(|error| error.into_inner())
            .clear();
        Ok(())
    }

    /// Returns the layered profile of a process, caching it by process key.
    pub fn layered_for(&self, process: &Process) -> Option<Arc<LayeredProfile>> {
        let key = process.key().to_string();
        if let Some(profile) = self
            .layered
            .read()
            .unwrap_or_else(|error| error.into_inner())
            .get(&key)
        {
            return Some(Arc::clone(profile));
        }

        let profiles = self
            .profiles
            .read()
            .unwrap_or_else(|error| error.into_inner());
        let matching = pm_profile::find_matching_profile(&profiles, &process.matching_data())?;
        let layered = Arc::new(LayeredProfile::new(
            Arc::new(matching.profile.clone()),
            &self.config,
            &self.global,
        ));
        drop(profiles);
        self.layered
            .write()
            .unwrap_or_else(|error| error.into_inner())
            .insert(key, Arc::clone(&layered));
        Some(layered)
    }

    /// Returns the store, used to load outdated profiles.
    pub fn store(&self) -> &ProfileStore {
        &self.store
    }

    /// Returns the layered view of a local profile by its ID, used by the
    /// netquery history retention callback.
    pub fn layered_for_id(&self, id: &str) -> Option<Arc<LayeredProfile>> {
        let profile = self.store.get(id).ok()?;
        Some(Arc::new(LayeredProfile::new(
            Arc::new(profile),
            &self.config,
            &self.global,
        )))
    }
}

impl ProfileLookup for CoreProfiles {
    fn local_profile(&self, process: &Process) -> Option<LocalProfileInfo> {
        let layered = self.layered_for(process)?;
        let local = layered.local_profile();
        Some(LocalProfileInfo {
            id: local.id.clone(),
            name: local.name.clone(),
            source: local.source.clone(),
            internal: local.internal,
            enable_history: layered.enable_history(),
            revision_cnt: layered.revision(),
        })
    }

    fn profile_source(&self, process: &Process, _option_key: &str) -> Option<String> {
        // The layered profile does not expose which layer supplied an option;
        // the local profile is the best available source for the reason.
        self.layered_for(process)
            .map(|layered| layered.local_profile().id.clone())
    }
}

impl LayeredProfileLookup for CoreProfiles {
    fn layered_profile(&self, process: &Process) -> Option<Arc<LayeredProfile>> {
        self.layered_for(process)
    }
}

impl SplitTunnelProfiles for CoreProfiles {
    fn profile_revision(&self, process: &Process) -> Option<u64> {
        self.layered_for(process).map(|layered| {
            if layered.needs_update() {
                layered.update(|id| {
                    self.store
                        .get(id)
                        .map_err(|error| error.to_string())
                })
            } else {
                layered.revision()
            }
        })
    }

    fn use_split_tun(&self, process: &Process) -> bool {
        self.layered_for(process)
            .is_some_and(|layered| layered.use_split_tun())
    }

    fn split_tun_proxy(&self, process: &Process) -> String {
        self.layered_for(process)
            .map(|layered| layered.split_tun_proxy())
            .unwrap_or_default()
    }

    fn split_tun_interface(&self, process: &Process) -> String {
        self.layered_for(process)
            .map(|layered| layered.split_tun_interface())
            .unwrap_or_default()
    }

    fn match_split_tun_usage_policy(
        &self,
        process: &Process,
        entity: &mut pm_intel::Entity,
    ) -> pm_endpoints::EpResult {
        self.layered_for(process)
            .map(|layered| layered.match_split_tun_usage_policy(entity).0)
            .unwrap_or(pm_endpoints::EpResult::NoMatch)
    }
}

/// Environment of the core firewall.
pub struct CoreFirewallEnv {
    own_pid: i32,
    addresses: Arc<pm_netenv::AddressCache>,
    custom_lists: ServiceCustomLists,
    profiles: Arc<CoreProfiles>,
    resolver: Arc<ResolverService>,
    unbreak_lists: Arc<pm_intel::FilterLists>,
    ip_info: Arc<IpInfoStore>,
    split_tunnel: Arc<SplitTunnelEnvAdapter>,
    api_endpoint: Option<(IpAddr, u16)>,
    filter_enabled: AtomicBool,
    permanent_verdicts: AtomicBool,
    dns_interception: AtomicBool,
    /// Interoperability verdicts, registered lazily by the interop module.
    interop: Arc<pm_interop::Interop>,
    verdict_enabled: Arc<AtomicBool>,
}

impl CoreFirewallEnv {
    /// Builds the environment from the core modules and the split tunnel
    /// adapter.
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        config: &Arc<Config>,
        local_addr: std::net::SocketAddr,
        resolver: Arc<ResolverService>,
        unbreak_lists: Arc<pm_intel::FilterLists>,
        custom_lists: ServiceCustomLists,
        profiles: Arc<CoreProfiles>,
        split_tunnel: Arc<SplitTunnelEnvAdapter>,
        ip_info: Arc<IpInfoStore>,
        interop: Arc<pm_interop::Interop>,
        verdict_enabled: Arc<AtomicBool>,
    ) -> CoreFirewallEnv {
        let api_endpoint = local_addr.ip().is_loopback().then(|| (local_addr.ip(), local_addr.port()));

        CoreFirewallEnv {
            own_pid: std::process::id() as i32,
            addresses: Arc::clone(&pm_netenv::network().addresses),
            custom_lists,
            profiles,
            resolver,
            unbreak_lists,
            ip_info,
            split_tunnel,
            api_endpoint,
            filter_enabled: AtomicBool::new(config.get_as_bool("filter/enable", true)()),
            permanent_verdicts: AtomicBool::new(
                config.get_as_bool("filter/permanentVerdicts", true)(),
            ),
            dns_interception: AtomicBool::new(
                config.get_as_bool("filter/dnsQueryInterception", true)(),
            ),
            interop,
            verdict_enabled,
        }
    }

    /// Refreshes the option flags after a config change.
    pub fn refresh_options(&self, config: &Arc<Config>) {
        self.filter_enabled
            .store(config.get_as_bool("filter/enable", true)(), Ordering::SeqCst);
        self.permanent_verdicts.store(
            config.get_as_bool("filter/permanentVerdicts", true)(),
            Ordering::SeqCst,
        );
        self.dns_interception.store(
            config.get_as_bool("filter/dnsQueryInterception", true)(),
            Ordering::SeqCst,
        );
    }

    /// Returns whether the privacy filter is enabled.
    pub fn filter_enabled(&self) -> bool {
        self.filter_enabled.load(Ordering::SeqCst)
    }

    /// Returns the IP info store for the DNS hook.
    pub fn ip_info(&self) -> &Arc<IpInfoStore> {
        &self.ip_info
    }

    /// Returns the profiles for the DNS hook.
    pub fn profiles(&self) -> &Arc<CoreProfiles> {
        &self.profiles
    }
}

impl FirewallEnv for CoreFirewallEnv {
    fn own_pid(&self) -> i32 {
        self.own_pid
    }

    fn is_my_ip(&self, ip: IpAddr) -> Result<bool, String> {
        self.addresses.is_my_ip(ip)
    }

    fn local_network(&self, ip: IpAddr) -> Result<Option<LocalNetwork>, String> {
        Ok(self
            .addresses
            .get_local_network(ip)?
            .map(|network| LocalNetwork {
                ip: network.network,
                prefix_len: network.prefix_len,
            }))
    }

    fn multicast_request(
        &self,
        _conn: &pm_network::Connection,
        _network: &LocalNetwork,
    ) -> Option<MulticastRequest> {
        // Go `network.GetMulticastRequestConn` is not ported yet.
        None
    }

    fn online_status(&self) -> OnlineStatus {
        pm_netenv::shared().online_status()
    }

    fn is_connectivity_domain(&self, domain: &str) -> bool {
        let portal_domain = pm_netenv::shared()
            .captive_portal()
            .map(|portal| portal.domain)
            .unwrap_or_default();
        pm_netenv::is_connectivity_domain(domain, &portal_domain)
    }

    fn resolver_disabled(&self) -> bool {
        self.resolver.is_disabled()
    }

    fn unbreak_filter_list_ids(&self) -> Vec<String> {
        pm_intel::unbreak_filter_list_ids(self.unbreak_lists.cache())
            .ok()
            .flatten()
            .unwrap_or_default()
    }

    fn custom_lists(&self) -> &dyn CustomLists {
        &self.custom_lists
    }

    fn load_local_profile(&self, id: &str) -> Result<Profile, String> {
        self.profiles.store().get(id).map_err(|error| error.to_string())
    }

    fn prompt(&self, conn: &mut pm_network::Connection) {
        // The interactive prompt needs an async wait; the synchronous firewall
        // hook currently returns the same verdict Go gives after its timeout.
        conn.deny(
            "prompting in progress, please respond to prompt",
            "filter/defaultAction",
        );
    }
}

impl PacketEnv for CoreFirewallEnv {
    fn filter_enabled(&self) -> bool {
        self.filter_enabled.load(Ordering::SeqCst)
    }

    fn permanent_verdicts(&self) -> bool {
        self.permanent_verdicts.load(Ordering::SeqCst)
    }

    fn dns_query_interception(&self) -> bool {
        self.dns_interception.load(Ordering::SeqCst)
    }

    fn api_endpoint(&self) -> Option<(IpAddr, u16)> {
        self.api_endpoint
    }

    fn ipv6_enabled(&self) -> bool {
        self.addresses
            .get_assigned_addresses()
            .map(|(_, ipv6)| !ipv6.is_empty())
            .unwrap_or(false)
    }

    fn is_proxied_split_tunnel_connection(&self, conn: &pm_network::Connection) -> bool {
        self.split_tunnel.is_proxied_connection_info(conn)
    }

    fn check_split_tunneling(&self, conn: &mut pm_network::Connection) {
        pm_firewall::check_split_tunneling(conn, self.split_tunnel.as_ref());
    }

    fn request_split_tunneling(&self, conn: &mut pm_network::Connection) -> Result<(), String> {
        pm_firewall::request_split_tunneling(conn, self.split_tunnel.as_ref())
    }

    fn update_ips_and_cnames(
        &self,
        query: &pm_resolver::Query,
        rr: &pm_resolver::RRCache,
        conn: &pm_network::Connection,
    ) {
        let Some(profile) = self
            .profiles
            .layered_for(conn.process.as_ref().expect("process"))
        else {
            return;
        };
        pm_firewall::dns_filter::update_ips_and_cnames(
            &self.ip_info,
            &profile,
            query,
            rr,
            conn,
        );
    }

    fn external_verdict(&self, conn: &pm_network::Connection) -> Option<ExternalVerdict> {
        if !self.verdict_enabled.load(Ordering::SeqCst) {
            return None;
        }
        let decision = self.interop.verdict_handler(conn);
        if decision.is_undecided() {
            return None;
        }
        Some(ExternalVerdict {
            verdict: decision.verdict,
            reason: decision.reason,
            skip_tunnel: decision.skip_tunnel,
        })
    }

    fn remove_prompt(&self, conn: &SharedConnection) {
        conn.lock()
            .unwrap_or_else(|error| error.into_inner())
            .remove_prompt();
    }
}

/// Bandwidth updates from the packet handler to netquery.
pub struct NetqueryBandwidth {
    db: Arc<pm_netquery::Database>,
}

impl NetqueryBandwidth {
    /// Creates the sink.
    pub fn new(db: Arc<pm_netquery::Database>) -> NetqueryBandwidth {
        NetqueryBandwidth { db }
    }
}

impl BandwidthSink for NetqueryBandwidth {
    fn update_bandwidth(
        &self,
        history_enabled: bool,
        _profile: &str,
        process_key: &str,
        conn_id: &str,
        bytes_received: u64,
        bytes_sent: u64,
    ) -> Result<(), String> {
        self.db
            .update_bandwidth(
                history_enabled,
                process_key,
                conn_id,
                bytes_received,
                bytes_sent,
                pm_database::now(),
            )
            .map_err(|error| error.to_string())
    }
}

/// Dependencies of the firewall, supplied by the core service.
pub struct FirewallDeps<'a> {
    pub config: Arc<Config>,
    pub databases: Arc<pm_database::Databases>,
    pub runtime: &'a pm_runtime::Runtime,
    pub data_dir: std::path::PathBuf,
    pub local_addr: std::net::SocketAddr,
    pub custom_lists: Arc<pm_intel::customlists::CustomListService>,
    pub filter_lists: Arc<pm_intel::FilterLists>,
    pub geoip: Arc<pm_geoip::GeoIp>,
    pub resolver: Arc<ResolverService>,
    pub profiles: Arc<CoreProfiles>,
    pub split_tunnel: Arc<SplitTunnelEnvAdapter>,
    pub ip_info: Arc<IpInfoStore>,
    /// Interoperability module that may supply external verdicts.
    pub interop: Arc<pm_interop::Interop>,
    /// Lazily set by the interop module when it registers its verdict handler.
    pub verdict_enabled: Arc<AtomicBool>,
    /// Optional file for packet verdict metrics.
    pub packet_metrics_file: Option<std::path::PathBuf>,
}

/// Everything the live firewall needs at runtime.
pub struct Firewall {
    pub env: Arc<CoreFirewallEnv>,
    pub profiles: Arc<CoreProfiles>,
    pub network: Arc<Network>,
    pub handler: Arc<PacketHandler>,
    pub netquery: Arc<pm_netquery::Manager>,
    pub netquery_db: Arc<pm_netquery::Database>,
    pub conntrack: Arc<dyn ConntrackControl>,
    pub metrics: Arc<pm_firewall::PacketMetrics>,
    cancel: CancellationToken,
    tasks: Mutex<Vec<tokio::task::JoinHandle<()>>>,
    subscription: Mutex<Option<pm_database::Subscription>>,
    started: AtomicBool,
    metrics_enabled: bool,
}

impl Firewall {
    /// Builds the firewall components, without touching the network stack.
    pub fn new(deps: FirewallDeps<'_>) -> anyhow::Result<Firewall> {
        let FirewallDeps {
            config,
            databases,
            runtime,
            data_dir,
            local_addr,
            custom_lists,
            filter_lists,
            geoip,
            resolver,
            profiles,
            split_tunnel,
            ip_info,
            interop,
            verdict_enabled,
            packet_metrics_file,
        } = deps;

        let custom_lists = ServiceCustomLists::new(custom_lists);
        let env = Arc::new(CoreFirewallEnv::new(
            &config,
            local_addr,
            resolver,
            Arc::clone(&filter_lists),
            custom_lists,
            Arc::clone(&profiles),
            split_tunnel,
            Arc::clone(&ip_info),
            interop,
            verdict_enabled,
        ));

        let processes: Arc<dyn ProcessResolver> = Arc::new(SystemProcessResolver::new(
            ProcessCache::with_default_tag_handlers(ProcReader::new(pm_netstate::ProcFs::default())),
            Arc::new(pm_netstate::NetState::new(pm_netstate::ProcFs::default())),
        ));
        let lists = Arc::clone(&filter_lists);
        let geoip_for_entities = Arc::clone(&geoip);
        let network_env = Environment::new(
            processes,
            Arc::new(move || {
                pm_intel::Entity::new(Arc::clone(&lists), Arc::clone(&geoip_for_entities) as Arc<dyn pm_intel::LocationProvider>)
            }),
        )
        .with_profiles(profiles.clone() as Arc<dyn ProfileLookup>)
        .with_domains(Arc::new(pm_firewall::dns_filter::IpInfoDomains {
            store: Arc::clone(&env.ip_info),
        }) as Arc<dyn pm_network::DomainLookup>)
        .with_captive_portal(Arc::new(|| {
            pm_netenv::shared().captive_portal().map(|portal| {
                pm_network::CaptivePortal {
                    ip: portal.ip.unwrap_or(std::net::IpAddr::V4(std::net::Ipv4Addr::UNSPECIFIED)),
                    domain: portal.domain,
                }
            })
        }));
        let network = Network::new(network_env);
        pm_network::register_as_database(&network, &databases)
            .map_err(|error| anyhow::anyhow!("register network database: {error}"))?;

        let history_file = data_dir.join("databases/history.db");
        let db = Arc::new(
            pm_netquery::Database::open(&history_file)
                .map_err(|error| anyhow::anyhow!("open netquery database: {error}"))?,
        );
        let manager = Arc::new(
            pm_netquery::Manager::new(
                Arc::clone(&db),
                pm_netquery::PUSH_PREFIX,
                runtime.registry(),
            )
            .map_err(|error| anyhow::anyhow!("register netquery push: {error}"))?,
        );

        let handler = Arc::new(
            PacketHandler::new(
                Arc::clone(&network),
                env.clone() as Arc<dyn PacketEnv>,
                Arc::clone(&profiles) as Arc<dyn LayeredProfileLookup>,
                Arc::new(pm_firewall::PreAuthPorts::new()),
                Arc::new(SystemConntrack::new()) as Arc<dyn ConntrackControl>,
            )
            .with_bandwidth_sink(Arc::new(NetqueryBandwidth::new(Arc::clone(&db)))),
        );

        Ok(Firewall {
            env,
            profiles: Arc::clone(&profiles),
            network,
            handler,
            netquery: manager,
            netquery_db: db,
            conntrack: Arc::new(SystemConntrack::new()),
            metrics: Arc::new(pm_firewall::PacketMetrics::new(
                packet_metrics_file
                    .clone()
                    .unwrap_or_else(|| data_dir.join("packet-metrics.txt")),
            )),
            metrics_enabled: packet_metrics_file.is_some(),
            cancel: CancellationToken::new(),
            tasks: Mutex::new(Vec::new()),
            subscription: Mutex::new(Some(
                databases
                    .interface(pm_database::Options {
                        local: true,
                        internal: true,
                        ..pm_database::Options::default()
                    })
                    .subscribe(pm_database::Query::new("network:"))
                    .map_err(|error| anyhow::anyhow!("subscribe to network updates: {error}"))?,
            )),
            started: AtomicBool::new(false),
        })
    }

    fn take_subscription(&self) -> Option<pm_database::Subscription> {
        self.subscription
            .lock()
            .unwrap_or_else(|error| error.into_inner())
            .take()
    }

    /// Returns whether the firewall is running.
    pub fn is_running(&self) -> bool {
        self.started.load(Ordering::SeqCst)
    }

    /// Installs iptables and starts the four nfqueue readers.
    ///
    /// Requires root/CAP_NET_ADMIN. When privileges are missing the firewall
    /// stays unstarted and a warning is logged; the rest of the core keeps
    /// working.
    #[cfg(target_os = "linux")]
    pub fn start(&self) -> anyhow::Result<()> {
        if !self.env.filter_enabled() {
            tracing::info!("filter: privacy filter is disabled");
            return Ok(());
        }
        if !is_root() {
            tracing::warn!("filter: not starting interception, root privileges are required");
            return Ok(());
        }
        if self.started.swap(true, Ordering::SeqCst) {
            return Ok(());
        }

        use pm_interception::iptables::{self, Cmd};
        iptables::activate(&Cmd::v4(), iptables::V4_RULES, iptables::ONCE, iptables::V4_CHAINS)
            .map_err(|error| anyhow::anyhow!("activate IPv4 iptables: {error}"))?;
        if self.env.ipv6_enabled() {
            iptables::activate(
                &Cmd::v6(),
                iptables::V6_RULES,
                iptables::ONCE,
                iptables::V6_CHAINS,
            )
            .map_err(|error| anyhow::anyhow!("activate IPv6 iptables: {error}"))?;
        }

        let (tx, mut rx) = mpsc::channel::<pm_interception::queue::QueuedPacket>(1024);
        let mut queues = Vec::new();
        for (number, inbound) in [
            (iptables::QUEUE_V4_OUT, false),
            (iptables::QUEUE_V4_IN, true),
        ] {
            queues.push(pm_interception::queue::start(number, inbound, tx.clone())?);
        }
        if self.env.ipv6_enabled() {
            for (number, inbound) in [
                (iptables::QUEUE_V6_OUT, false),
                (iptables::QUEUE_V6_IN, true),
            ] {
                queues.push(pm_interception::queue::start(number, inbound, tx.clone())?);
            }
        }
        drop(tx);

        let queue_numbers: Vec<u16> = queues.iter().map(|handle| handle.queue_number()).collect();
        tracing::info!(
            queues = ?queue_numbers,
            "filter: nfqueue interception started"
        );

        let handler = Arc::clone(&self.handler);
        let cancel = self.cancel.clone();
        let metrics = Arc::clone(&self.metrics);
        let feed = self.netquery.clone();
        let dispatch = tokio::spawn(async move {
            loop {
                tokio::select! {
                    () = cancel.cancelled() => break,
                    packet = rx.recv() => {
                        let Some(packet) = packet else { break };
                        handler.dispatch(Box::new(
                            NfqPacket::new(packet).with_metrics(metrics.recorder()),
                        ));
                    }
                }
            }
        });

        let cancel = self.cancel.clone();
        let stop = tokio::spawn(async move {
            cancel.cancelled().await;
            for queue in queues {
                let _ = queue.stop().await;
            }
        });

        let cancel = self.cancel.clone();
        let manager = Arc::clone(&feed);
        let network = Arc::clone(&self.network);
        let mut subscription = self
            .take_subscription()
            .expect("network subscription");
        let handle_feed = tokio::spawn(async move {
            loop {
                tokio::select! {
                    () = cancel.cancelled() => return,
                    update = subscription.recv() => {
                        let Some(update) = update else { return };
                        let Some(conn) = network.get_connection(&update.key.key) else {
                            continue;
                        };
                        let manager = Arc::clone(&manager);
                        if let Err(error) = tokio::task::spawn_blocking(move || manager.handle(&conn)).await {
                            tracing::error!(%error, "netquery: connection handler panicked");
                        }
                    }
                }
            }
        });

        let metrics_writer = {
            let metrics = Arc::clone(&self.metrics);
            let cancel = self.cancel.clone();
            if self.metrics_enabled {
                let _ = metrics.enable();
                Some(tokio::spawn(async move { metrics.run(cancel).await }))
            } else {
                None
            }
        };

        let mut tasks = self
            .tasks
            .lock()
            .unwrap_or_else(|error| error.into_inner());
        tasks.extend([dispatch, stop, handle_feed]);
        if let Some(task) = metrics_writer {
            tasks.push(task);
        }
        Ok(())
    }

    /// Stops interception and restores the iptables state.
    #[cfg(target_os = "linux")]
    pub async fn stop(&self) -> anyhow::Result<()> {
        if !self.started.swap(false, Ordering::SeqCst) {
            return Ok(());
        }
        self.cancel.cancel();
        self.metrics.disable();
        let tasks: Vec<_> = std::mem::take(
            &mut *self.tasks.lock().unwrap_or_else(|error| error.into_inner()),
        );
        for task in tasks {
            task.abort();
        }
        use pm_interception::iptables::{self, Cmd};
        if let Err(error) = iptables::deactivate(&Cmd::v4(), iptables::ONCE, iptables::V4_CHAINS) {
            tracing::warn!(%error, "filter: failed to clean up IPv4 iptables");
        }
        if self.env.ipv6_enabled() {
            if let Err(error) = iptables::deactivate(&Cmd::v6(), iptables::ONCE, iptables::V6_CHAINS) {
                tracing::warn!(%error, "filter: failed to clean up IPv6 iptables");
            }
        }
        Ok(())
    }
}

#[cfg(target_os = "linux")]
fn is_root() -> bool {
    nix::unistd::geteuid().is_root()
}
