//! Service startup: databases, configuration and the API.

use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_api::{Api, UiArchives};
use pm_config::{Config, ConfigOption, PossibleValue};
use pm_database::{DatabaseInfo, Databases};
use serde_json::{json, Value};
use tokio::net::TcpListener;
use tokio_util::sync::CancellationToken;

use crate::api_auth::{ApiAuthenticator, SystemLookup};
use crate::cli::Options;

/// Configuration keys of the core.
pub const CFG_RELEASE_CHANNEL: &str = "core/releaseChannel";
/// Whether automatic software updates are enabled.
pub const CFG_AUTOMATIC_UPDATES: &str = "core/automaticUpdates";
/// Whether automatic intel data updates are enabled.
pub const CFG_AUTOMATIC_INTEL_UPDATES: &str = "core/automaticIntelUpdates";
pub const CFG_DEV_MODE: &str = "core/devMode";
pub const CFG_LOG_LEVEL: &str = "core/log/level";
pub const CFG_LISTEN_ADDRESS: &str = "core/listenAddress";
pub const CFG_API_KEYS: &str = "core/apiKeys";

/// A running core service.
pub struct Service {
    pub config: Arc<Config>,
    pub databases: Arc<Databases>,
    pub api: Arc<Api>,
    /// Runtime value registry, injected as the `runtime` database.
    pub runtime: pm_runtime::Runtime,
    /// Notification registry, injected as the `notifications` database.
    pub notifications: Arc<pm_notifications::Notifications>,
    /// System status: module states and `runtime:system/status`.
    pub status: Arc<pm_status::Status>,
    /// Filter list intelligence on top of the cache database.
    pub filter_lists: Arc<pm_intel::FilterLists>,
    /// GeoIP databases; a future updater loads them.
    pub geoip: Arc<pm_geoip::GeoIp>,
    /// Global profile settings (filter rules and switches).
    pub profile_config: Arc<pm_profile::GlobalProfileConfig>,
    /// Resolver and nameserver.
    pub dns: Arc<crate::dns::Dns>,
    /// Custom filter list and its file watcher.
    pub custom_lists: Arc<pm_intel::customlists::CustomListService>,
    /// Binary updater (`index.json` in the bin dir).
    pub binary_updater: Arc<pm_updates::Updater>,
    /// Intel resource updater (`index.json` in the intel dir).
    pub intel_updater: Arc<pm_updates::Updater>,
    /// Filter lists module glue: cache loading, states and event handling.
    pub filter_lists_module: Arc<pm_intel::FilterListsModule>,
    /// Online status monitor.
    pub online_monitor: Arc<pm_netenv::OnlineMonitor>,
    /// Split tunnel interface environment backed by netenv.
    pub system_interfaces: Arc<pm_splittun::SystemInterfaceEnv>,
    /// Network change monitor.
    pub network_monitor: Arc<pm_netenv::NetworkChangeMonitor>,
    /// Optional Apprise notifications.
    pub apprise: Arc<crate::apprise::Apprise>,
    /// Estimated device locations.
    pub device_locations: Arc<crate::location::CoreDeviceLocations>,
    /// Firewall environment and packet pipeline.
    pub firewall: Arc<crate::firewall::Firewall>,
    /// Pause/resume control module.
    pub control: Arc<pm_control::Control<crate::control::CoreControlEnv>>,
    /// YAML-driven broadcast notifications module.
    pub broadcasts: Arc<pm_broadcasts::Broadcasts>,
    /// Triggered by a restart request (updater or API).
    pub restart_token: CancellationToken,
    /// Split tunnel proxies.
    pub split_proxies: Arc<pm_splittun::Proxies>,
    /// Pending split tunnel requests.
    pub split_queue: Arc<pm_splittun::RequestQueue>,
    /// Subscribed in `new` so changes before `run` are not missed.
    profile_changes: Mutex<Option<tokio::sync::broadcast::Receiver<()>>>,
    listener: Mutex<Option<TcpListener>>,
    local_addr: std::net::SocketAddr,
    data_dir: PathBuf,
}

impl Service {
    /// Starts the core: prepares directories, databases, configuration and the
    /// API listener.
    pub async fn start(options: &Options) -> anyhow::Result<Service> {
        let data_dir = options.data_dir.clone();
        let databases_dir = data_dir.join("databases");
        std::fs::create_dir_all(&databases_dir)?;
        std::fs::create_dir_all(&options.log_dir)?;

        // Databases: the same two the Go core registers.
        let databases = Arc::new(Databases::new(&databases_dir)?);
        for (name, description) in [
            ("core", "Holds core data, such as settings and profiles"),
            ("cache", "Cached data, such as Intelligence and DNS Records"),
        ] {
            databases.register(DatabaseInfo {
                name: name.to_string(),
                description: description.to_string(),
                storage_type: "sqlite".to_string(),
                shadow_delete: false,
            })?;
            databases.controller(name)?;
        }

        // Data migrations, like Go `service/profile` runs at startup.
        let migration_db = pm_migration::internal_interface(&databases);
        if let Err(error) = pm_profile::migrations::migrate(&migration_db) {
            tracing::warn!(%error, "profile: migrations failed");
        }

        // Runtime values: modules expose computed records through `runtime`.
        let mut runtime = pm_runtime::Runtime::new();
        runtime.start(&databases)?;

        // Notifications: an injected database backed by the registry.
        let notifications = Arc::new(pm_notifications::Notifications::new());
        databases.register_injected("notifications", "Notifications")?;
        let notifications_controller = databases.inject(
            "notifications",
            Box::new(pm_notifications::NotificationsStorage::new(Arc::clone(
                &notifications,
            ))),
        )?;
        notifications.set_db_controller(notifications_controller);

        // System status publishes itself through the runtime registry.
        let status = Arc::new(pm_status::Status::new(
            pm_netenv::shared(),
            Arc::clone(&notifications),
        ));
        status.register(runtime.registry())?;

        // GeoIP databases start empty; the updater will load them.
        let geoip = Arc::new(pm_geoip::GeoIp::new());

        // Filter lists live in the cache database.
        let filter_lists = Arc::new(pm_intel::FilterLists::new(pm_intel::cache_interface(
            &databases,
        )));

        // Restart requests from the updater are handled as a graceful stop;
        // the service manager can then start the new binary.
        let restart_token = CancellationToken::new();
        let updater_restart = restart_token.clone();
        let service_restart_token = restart_token.clone();

        // Binary updater: checks the release channel, downloads and applies on
        // user action only, needs a restart.
        let mut binary_config = pm_updates::UpdaterConfig {
            name: pm_updates::DEFAULT_BINARY_INDEX_NAME.to_string(),
            directory: options.bin_dir.to_string_lossy().into_owned(),
            download_directory: data_dir
                .join("download_binaries")
                .to_string_lossy()
                .into_owned(),
            purge_directory: data_dir
                .join("upgrade_obsolete_binaries")
                .to_string_lossy()
                .into_owned(),
            index_urls: pm_updates::binary_update_url_list("stable"),
            index_file: "index.json".to_string(),
            verify: Some(pm_jess::binary_trust_store()),
            auto_check: true,
            auto_download: false,
            auto_apply: false,
            needs_restart: true,
            notify: true,
            ..pm_updates::UpdaterConfig::default()
        };
        #[cfg(target_os = "linux")]
        if let Ok(exe) = std::env::current_exe() {
            let exe = exe.to_string_lossy().into_owned();
            // Restore the SELinux context of the new core binary, like Go.
            binary_config.post_upgrade_commands.push(pm_updates::UpdateCommandConfig {
                command: "restorecon".to_string(),
                args: vec![exe.clone()],
                trigger_artifact_fname: exe,
                fail_on_error: false,
            });
        }
        let binary_updater = Arc::new(
            pm_updates::Updater::new(
                binary_config,
                Arc::new(pm_updates::HttpsClient::new()),
                env!("CARGO_PKG_VERSION"),
                Some(Arc::clone(&notifications)),
                Some(Arc::new(move || updater_restart.cancel())),
            )
            .map_err(|error| anyhow::anyhow!("create binary updater: {error}"))?,
        );

        // Intel resource updater and the filter lists module.
        let intel_updater = Arc::new(
            pm_updates::Updater::new(
                pm_updates::UpdaterConfig {
                    name: "Portmaster Intel".to_string(),
                    directory: data_dir.join("intel").to_string_lossy().into_owned(),
                    download_directory: data_dir
                        .join("download_intel")
                        .to_string_lossy()
                        .into_owned(),
                    purge_directory: data_dir
                        .join("upgrade_obsolete_intel")
                        .to_string_lossy()
                        .into_owned(),
                    index_urls: vec!["https://updates.safing.io/intel.v3.json".to_string()],
                    index_file: "index.json".to_string(),
                    verify: Some(pm_jess::intel_trust_store()),
                    auto_check: true,
                    auto_download: true,
                    auto_apply: true,
                    needs_restart: false,
                    notify: false,
                    ..pm_updates::UpdaterConfig::default()
                },
                Arc::new(pm_updates::HttpsClient::new()),
                env!("CARGO_PKG_VERSION"),
                None,
                None,
            )
            .map_err(|error| anyhow::anyhow!("create intel updater: {error}"))?,
        );
        let filter_lists_module = Arc::new(pm_intel::FilterListsModule::new(
            Arc::clone(&filter_lists),
            Arc::new(pm_mgr::StateMgr::new("FilterLists")),
        ));
        if let Err(error) = filter_lists_module.start() {
            tracing::warn!(%error, "intel/filterlists: module start failed");
        }


        // Network environment notifications (offline/limited/captive portal).
        pm_netenv::shared().set_notification_sink(Arc::new(
            crate::netenv_sink::NetEnvNotificationSink::new(Arc::clone(&notifications)),
        ));

        // Online status monitor: addresses, portal probe and (later) the DNS
        // check. The first check runs when the service starts serving.
        let online_monitor = Arc::new(pm_netenv::OnlineMonitor::new(
            pm_netenv::shared(),
            Arc::clone(&pm_netenv::network().addresses),
            Arc::new(pm_netenv::SystemPortalProbe::new()),
            None,
        ));

        // Device location estimation from addresses, GeoIP, traceroute and
        // the local time zone.
        let device_locations = Arc::new(
            crate::location::CoreLocationEnvironment::new(
                Arc::clone(&pm_netenv::network().addresses),
                Arc::clone(&geoip),
                pm_netenv::network_changed_flag(),
            )
            .into_service(),
        );

        // Network change monitor: checksum of the interfaces; a change
        // triggers an online status investigation.
        let online_monitor_for_changes = Arc::clone(&online_monitor);
        let network_monitor = Arc::new(pm_netenv::NetworkChangeMonitor::new(
            Arc::new(pm_netenv::SystemInterfaces),
            pm_netenv::shared(),
            Some(Arc::new(move || online_monitor_for_changes.trigger())),
        ));

        // Split tunnel interface lookups use the netenv caches.
        let system_interfaces = Arc::new(pm_splittun::SystemInterfaceEnv::new(
            Arc::clone(&pm_netenv::network().interfaces),
            Arc::clone(&pm_netenv::network().addresses),
        ));

        // Configuration.
        let config = Config::new();
        register_options(&config, options)?;
        pm_profile::options::register_options(&config)?;
        pm_firewall::register_options(&config)?;
        pm_splittun::register_options(&config)?;
        pm_intel::customlists::register_options(&config)?;
        crate::dns::register_options(&config, &options.nameserver_address)?;
        config.register_database(&databases)?;
        let config_file = data_dir.join("config.json");
        match config.start(&config_file) {
            Ok(outcome) => {
                for error in &outcome.validation_errors {
                    tracing::warn!(error = %error, "config: validation error");
                }
            }
            Err(pm_config::Error::Io(error)) if error.kind() == std::io::ErrorKind::NotFound => {}
            Err(error) => return Err(error.into()),
        }

        // Optional Apprise notifications.
        let apprise = crate::apprise::from_config(&config);

        // Apply the update settings from the config.
        let release_channel = config.get_as_string(CFG_RELEASE_CHANNEL, "stable")();
        let auto_software = config.get_as_bool(CFG_AUTOMATIC_UPDATES, true)();
        let auto_intel = config.get_as_bool(CFG_AUTOMATIC_INTEL_UPDATES, true)();
        binary_updater.configure(
            auto_software,
            pm_updates::binary_update_url_list(&release_channel),
        );
        intel_updater.configure(auto_intel, pm_updates::intel_update_urls());

        let profile_changes = config.subscribe_changes();
        let profile_config = Arc::new(pm_profile::GlobalProfileConfig::new(&config));
        if let Err(error) = profile_config.refresh() {
            tracing::warn!(%error, "profile: some global settings are invalid");
        }

        // The config file is stronger than the command line flags.
        let listen_address = config.get_as_string(CFG_LISTEN_ADDRESS, &options.api_address)();
        let dev_mode = config.get_as_bool(CFG_DEV_MODE, options.dev_mode)();
        let api_keys = config.get_as_string_array(CFG_API_KEYS, &[])();

        let api = Api::new(&listen_address);
        api.set_dev_mode(dev_mode);
        api.set_databases(Arc::clone(&databases));
        api.register_config_endpoints(Arc::clone(&config));
        api.update_api_keys(&api_keys);
        api.set_ui_archives(Arc::new(UiArchives::new(&options.bin_dir)));
        pm_api::ui::register_ui_endpoints(&api)
            .map_err(|error| anyhow::anyhow!("register ui api: {error}"))?;

        // The remaining core endpoints: shutdown and directory map.
        let shutdown_token = service_restart_token.clone();
        api.register_endpoint(
            pm_api::Endpoint::new("core/shutdown", "Shut Down Portmaster", "Shut down the Portmaster Core Service and all UI components.")
                .with_write(pm_api::PERMIT_SELF)
                .with_action(move |_| {
                    shutdown_token.cancel();
                    Ok("shutdown initiated".to_string())
                }),
        )
        .map_err(|error| anyhow::anyhow!("register shutdown api: {error}"))?;

        let bin_dir = options.bin_dir.to_string_lossy().into_owned();
        let data_dir_str = data_dir.to_string_lossy().into_owned();
        let log_dir = options.log_dir.to_string_lossy().into_owned();
        api.register_endpoint(
            pm_api::Endpoint::new("core/paths", "Get Directories", "Returns the directories used by Portmaster: binaries (bin), variable data (data) and log files (logs).")
                .with_read(pm_api::PERMIT_USER)
                .with_struct(move |_| Ok(json!({
                    "bin": bin_dir,
                    "data": data_dir_str,
                    "logs": log_dir,
                }))),
        )
        .map_err(|error| anyhow::anyhow!("register paths api: {error}"))?;

        // Restart endpoint; a restart is a graceful stop for the supervisor.
        let api_restart = service_restart_token.clone();
        api.register_endpoint(
            pm_api::Endpoint::new("core/restart", "Restart Portmaster", "Restart the Portmaster Core Service.")
                .with_write(pm_api::PERMIT_ADMIN)
                .with_action(move |_| {
                    api_restart.cancel();
                    Ok("restart initiated".to_string())
                }),
        )
        .map_err(|error| anyhow::anyhow!("register restart api: {error}"))?;

        // The country table is static, the endpoint is always available.
        pm_geoip::register_api(&api)
            .map_err(|error| anyhow::anyhow!("register geoip api: {error}"))?;

        // Custom filter list: option, endpoint and background reloader.
        let custom_lists = Arc::new(pm_intel::customlists::CustomListService::with_reporting(
            Some(Arc::clone(&notifications)),
            Some(Arc::new(pm_mgr::StateMgr::new("CustomList"))),
        ));
        custom_lists.set_file_path(
            &config.get_as_string(
                pm_intel::customlists::CFG_OPTION_CUSTOM_LIST_FILE_KEY,
                "",
            )(),
        );
        pm_intel::customlists::register_api(&api, Arc::clone(&custom_lists))
            .map_err(|error| anyhow::anyhow!("register customlists api: {error}"))?;

        register_update_endpoints(
            &api,
            Arc::clone(&binary_updater),
            Arc::clone(&intel_updater),
            Arc::clone(&filter_lists),
            Arc::clone(&filter_lists_module),
            Arc::clone(&geoip),
            api.ui_archives(),
        )?;

        let listener = TcpListener::bind(&listen_address)
            .await
            .map_err(|error| anyhow::anyhow!("failed to listen on {listen_address}: {error}"))?;
        tracing::info!(address = %listener.local_addr()?, "api: listening");

        // Authenticate local clients by their process; the server address is
        // the bound one, so a port of 0 works.
        api.set_authenticator(Arc::new(ApiAuthenticator::new(
            Arc::new(SystemLookup::new()),
            options.bin_dir.clone(),
            options.allowed_clients.clone(),
            listener.local_addr()?,
        )));

        // A nameserver that cannot listen is reported by a notification only.
        let dns = Arc::new(
            crate::dns::Dns::start(
                &config,
                &databases,
                &notifications,
                &api,
                &options.nameserver_address,
            )
            .await?,
        );

        // Profiles from the `core` database and the IP-info store.
        let profile_store = pm_profile::ProfileStore::new(
            databases.interface(pm_database::Options {
                local: true,
                internal: true,
                ..pm_database::Options::default()
            }),
        );
        let profiles = Arc::new(crate::firewall::CoreProfiles::new(
            Arc::clone(&config),
            Arc::clone(&profile_config),
            profile_store,
        )
        .map_err(|error| anyhow::anyhow!("load local profiles: {error}"))?);
        let ip_info = Arc::new(pm_resolver::IpInfoStore::new(
            databases.interface(pm_resolver::ipinfo::ip_info_interface_options()),
        ));

        // Split tunnel request queue and proxies. The proxies are only started
        // when the split tunnel module is enabled.
        let split_queue = Arc::new(pm_splittun::RequestQueue::new());
        let split_proxies = Arc::new(pm_splittun::Proxies::new());
        let split_env = Arc::new(pm_splittun::SplitTunnelEnvAdapter::new(
            std::process::id() as i32,
            Arc::clone(&split_queue),
            Arc::clone(&split_proxies),
            Arc::clone(&system_interfaces) as Arc<dyn pm_splittun::InterfaceEnv>,
            Arc::clone(&profiles) as Arc<dyn pm_splittun::SplitTunnelProfiles>,
        ));

        let firewall = Arc::new(crate::firewall::Firewall::new(
            crate::firewall::FirewallDeps {
                config: Arc::clone(&config),
                databases: Arc::clone(&databases),
                runtime: &runtime,
                data_dir: data_dir.clone(),
                local_addr: listener.local_addr()?,
                custom_lists: Arc::clone(&custom_lists),
                filter_lists: Arc::clone(&filter_lists),
                geoip: Arc::clone(&geoip),
                resolver: Arc::new(dns.resolver().clone()),
                profiles,
                split_tunnel: split_env,
                ip_info,
                packet_metrics_file: options.packet_metrics_file.clone(),
            },
        )?);



        // Go service/control: pause/resume with automatic resume.
        let control = Arc::new(pm_control::Control::new(
            crate::control::CoreControlEnv::new(
                Arc::clone(&config),
                Arc::clone(&firewall),
                service_restart_token.clone(),
            ),
            Arc::new(crate::control::CoreNotificationSink(Arc::clone(&notifications)))
                as Arc<dyn pm_control::NotificationSink>,
        ));
        crate::control::register_api(&api, Arc::clone(&control))
            .map_err(|error| anyhow::anyhow!("register control api: {error}"))?;

        // Broadcast notifications: the YAML source comes from the intel
        // updater and `run()` starts the 10-minute notifier worker.
        let broadcasts_env = Arc::new(crate::broadcasts::CoreBroadcastEnvironment::new(
            Arc::clone(&config),
            Arc::clone(&binary_updater),
            Arc::clone(&intel_updater),
            Arc::clone(&device_locations),
        ));
        let broadcasts = Arc::new(pm_broadcasts::Broadcasts::new(
            databases.interface(pm_database::Options {
                local: true,
                internal: true,
                ..pm_database::Options::default()
            }),
            Arc::clone(&notifications),
            broadcasts_env,
            Arc::new(crate::broadcasts::IntelBroadcastFileSource::new(Arc::clone(
                &intel_updater,
            ))),
            Arc::new(crate::broadcasts::CoreBroadcastAttachment),
        ));
        broadcasts
            .register_api(&api)
            .map_err(|error| anyhow::anyhow!("register broadcasts api: {error}"))?;
        if let Err(error) = broadcasts.ensure_install_info() {
            tracing::warn!(%error, "broadcasts: failed to ensure install info");
        }

        // Netquery API on top of the live connection database.
        let retention_profiles = Arc::clone(&firewall.profiles);
        let retention_config = Arc::clone(&config);
        let retention = Arc::new(move |profile: &str| {
            if let Some(id) = profile.strip_prefix("local/") {
                if let Some(layered) = retention_profiles.layered_for_id(id) {
                    return layered.keep_history();
                }
            }
            retention_config.get_as_int("history/keep", 30)()
        });
        crate::account_api::register_api(&api)
            .map_err(|error| anyhow::anyhow!("register account api: {error}"))?;

        crate::core_api::register_api(
            &api,
            Arc::clone(&config),
            Arc::clone(&notifications),
            Arc::clone(&firewall.netquery_db),
            Arc::clone(&firewall.profiles),
        )
        .map_err(|error| anyhow::anyhow!("register core api: {error}"))?;

        pm_netquery::api::register_api(
            &api,
            Arc::clone(&firewall.netquery_db),
            pm_netquery::api::ApiOptions::new(Arc::new(move || dev_mode), retention),
        )
        .map_err(|error| anyhow::anyhow!("register netquery api: {error}"))?;

        api.set_ready(true);

        Ok(Service {
            config,
            databases,
            api,
            runtime,
            notifications,
            status,
            filter_lists,
            geoip,
            profile_config,
            dns,
            custom_lists,
            binary_updater,
            intel_updater,
            filter_lists_module,
            online_monitor,
            system_interfaces,
            network_monitor,
            apprise,
            device_locations,
            firewall,
            control,
            broadcasts,
            restart_token,
            split_proxies: Arc::clone(&split_proxies),
            split_queue: Arc::clone(&split_queue),
            profile_changes: Mutex::new(Some(profile_changes)),
            local_addr: listener.local_addr()?,
            listener: Mutex::new(Some(listener)),
            data_dir,
        })
    }

    /// Returns the bound API address.
    pub fn local_addr(&self) -> std::net::SocketAddr {
        self.local_addr
    }

    /// Returns the addresses the nameserver listens on.
    pub async fn nameserver_addrs(&self) -> Vec<std::net::SocketAddr> {
        self.dns.listen_addrs().await
    }

    /// Returns the data directory.
    pub fn data_dir(&self) -> &std::path::Path {
        &self.data_dir
    }

    /// Serves the API until the process is asked to stop.
    pub async fn run(self: Arc<Self>) -> anyhow::Result<()> {
        let api = Arc::clone(&self.api);
        let databases = Arc::clone(&self.databases);
        let listener = self
            .listener
            .lock()
            .expect("lock poisoned")
            .take()
            .ok_or_else(|| anyhow::anyhow!("service is already running"))?;

        let server = pm_api::serve(api, listener);

        // Keep the parsed global rules and the custom filter list in sync with
        // config changes.
        let profile_config = Arc::clone(&self.profile_config);
        let custom_lists = Arc::clone(&self.custom_lists);
        let binary_updater = Arc::clone(&self.binary_updater);
        let intel_updater = Arc::clone(&self.intel_updater);
        let config = Arc::clone(&self.config);
        let firewall_env = Arc::clone(&self.firewall.env);
        let firewall_profiles = Arc::clone(&self.firewall.profiles);
        let mut changes = self
            .profile_changes
            .lock()
            .expect("lock poisoned")
            .take()
            .ok_or_else(|| anyhow::anyhow!("service is already running"))?;
        let refresher = tokio::spawn(async move {
            while let Ok(()) | Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) =
                changes.recv().await
            {
                if let Err(error) = profile_config.refresh() {
                    tracing::warn!(%error, "profile: some global settings are invalid");
                }
                firewall_env.refresh_options(&config);
                if let Err(error) = firewall_profiles.refresh() {
                    tracing::warn!(%error, "profile: failed to reload local profiles");
                }
                custom_lists.set_file_path(
                    &config.get_as_string(
                        pm_intel::customlists::CFG_OPTION_CUSTOM_LIST_FILE_KEY,
                        "",
                    )(),
                );
                // Update settings may have changed.
                let release_channel = config.get_as_string(CFG_RELEASE_CHANNEL, "stable")();
                let auto_software = config.get_as_bool(CFG_AUTOMATIC_UPDATES, true)();
                let auto_intel = config.get_as_bool(CFG_AUTOMATIC_INTEL_UPDATES, true)();
                binary_updater.configure(
                    auto_software,
                    pm_updates::binary_update_url_list(&release_channel),
                );
                intel_updater.configure(auto_intel, pm_updates::intel_update_urls());
            }
        });

        // Network change checks and their feedback into the online monitor.
        let network_monitor = Arc::clone(&self.network_monitor);
        let network_worker = tokio::spawn(async move {
            network_monitor
                .run(tokio_util::sync::CancellationToken::new())
                .await;
        });
        let trigger_network_monitor = Arc::clone(&self.network_monitor);
        let apprise = Arc::clone(&self.apprise);
        let mut online_events = pm_netenv::shared()
            .online_status_events()
            .subscribe("network-change", 8);
        let online_feedback = tokio::spawn(async move {
            while let Some(status) = online_events.recv().await {
                trigger_network_monitor.trigger();
                apprise.online_status(status).await;
            }
        });

        // Online status checks: immediately, on triggers and after the
        // per-status delay.
        let online_monitor = Arc::clone(&self.online_monitor);
        let online_worker = tokio::spawn(async move {
            online_monitor
                .run(tokio_util::sync::CancellationToken::new())
                .await;
        });

        // Binary updates: check every hour (first after 15 seconds, like Go).
        let binary_worker = {
            let updater = Arc::clone(&self.binary_updater);
            tokio::spawn(async move {
                let start = tokio::time::Instant::now() + Duration::from_secs(15);
                let mut interval = tokio::time::interval_at(
                    start,
                    pm_updates::UPDATE_TASK_REPEAT_DURATION,
                );
                loop {
                    interval.tick().await;
                    if !updater.config().auto_check {
                        continue;
                    }
                    let updater = Arc::clone(&updater);
                    let _ = tokio::task::spawn_blocking(move || {
                        if let Err(error) = updater.update_check() {
                            tracing::warn!(%error, "updates/binaries: update check failed");
                        }
                    })
                    .await;
                }
            })
        };

        // Intel updates: check every hour (first after 15 seconds, like Go),
        // then apply the filter list files of the new index.
        let intel_worker = {
            let updater = Arc::clone(&self.intel_updater);
            let module = Arc::clone(&self.filter_lists_module);
            let lists = Arc::clone(&self.filter_lists);
            let geoip = Arc::clone(&self.geoip);
            tokio::spawn(async move {
                let start = tokio::time::Instant::now() + Duration::from_secs(15);
                let mut interval = tokio::time::interval_at(
                    start,
                    pm_updates::UPDATE_TASK_REPEAT_DURATION,
                );
                loop {
                    interval.tick().await;
                    if !updater.config().auto_check {
                        continue;
                    }
                    let updater = Arc::clone(&updater);
                    let module = Arc::clone(&module);
                    let lists = Arc::clone(&lists);
                    let geoip = Arc::clone(&geoip);
                    let result = tokio::task::spawn_blocking(move || {
                        module.handle_resources_updated(|| {
                            updater
                                .update_check()
                                .map_err(|error| error.to_string())?;
                            apply_intel_update(&updater, &lists, &geoip)
                        })
                    })
                    .await;
                    match result {
                        Ok(Ok(())) => {}
                        Ok(Err(error)) => {
                            tracing::warn!(%error, "intel/filterlists: update failed")
                        }
                        Err(error) => {
                            tracing::warn!(%error, "intel/filterlists: update task failed")
                        }
                    }
                }
            })
        };

        // Reload the custom filter list on start and every minute, like the Go
        // module worker (which delays the first run by 20 seconds).
        let list_watcher = {
            let custom_lists = Arc::clone(&self.custom_lists);
            let config = Arc::clone(&self.config);
            tokio::spawn(async move {
                let start = tokio::time::Instant::now() + Duration::from_secs(20);
                let mut interval = tokio::time::interval_at(start, Duration::from_secs(60));
                loop {
                    interval.tick().await;
                    custom_lists.set_file_path(
                        &config.get_as_string(
                            pm_intel::customlists::CFG_OPTION_CUSTOM_LIST_FILE_KEY,
                            "",
                        )(),
                    );
                    match custom_lists.check_and_update_configured() {
                        Ok(_) => {}
                        Err(error) => {
                            tracing::warn!(%error, "intel/customlists: reload failed");
                        }
                    }
                }
            })
        };

        // Broadcast notifications: check every 10 minutes, like Go
        // `module.mgr.Repeat("broadcast notifier", 10*time.Minute, ...)`.
        let broadcasts = Arc::clone(&self.broadcasts);
        let broadcasts_worker = tokio::spawn(async move {
            let start = tokio::time::Instant::now() + Duration::from_secs(10 * 60);
            let mut interval = tokio::time::interval_at(start, Duration::from_secs(10 * 60));
            loop {
                interval.tick().await;
                let broadcasts = Arc::clone(&broadcasts);
                let _ = tokio::task::spawn_blocking(move || {
                    if let Err(error) = broadcasts.run_once() {
                        tracing::warn!(%error, "broadcasts: notifier run failed");
                    }
                })
                .await;
            }
        });

        // Start the split tunnel proxies when the module is enabled.
        if self.config.get_as_bool("splittun/enable", false)() {
            if let Err(error) = self
                .split_proxies
                .start(self.split_queue.clone(), self.system_interfaces.as_ref())
                .await
            {
                tracing::error!(%error, "splittun: failed to start proxies");
            }
        }

        // Install iptables and start the packet readers when enabled and
        // privileged. This is a no-op otherwise.
        if let Err(error) = self.firewall.start() {
            tracing::error!(%error, "filter: failed to start interception");
        }

        let outcome: anyhow::Result<()> = tokio::select! {
            result = server => result.map_err(anyhow::Error::from),
            _ = shutdown_signal() => {
                tracing::info!("shutting down");
                Ok(())
            }
            _ = self.restart_token.cancelled() => {
                tracing::info!("restart requested, stopping the service");
                Ok(())
            }
            _ = maintenance(Arc::clone(&databases)) => Ok(()),
        };

        refresher.abort();
        network_worker.abort();
        online_feedback.abort();
        online_worker.abort();
        binary_worker.abort();
        intel_worker.abort();
        list_watcher.abort();
        broadcasts_worker.abort();
        self.split_proxies.stop();
        self.control.shutdown().await;
        if let Err(error) = self.firewall.stop().await {
            tracing::warn!(%error, "filter: failed to stop interception");
        }
        self.dns.shutdown().await;
        databases.shutdown()?;
        outcome
    }

    /// Shuts the service down.
    pub fn shutdown(&self) -> anyhow::Result<()> {
        self.dns.stop();
        self.databases.shutdown()?;
        Ok(())
    }
}

fn register_options(config: &Config, options: &Options) -> anyhow::Result<()> {
    config.register(
        ConfigOption::new(
            "Apprise API URL",
            crate::apprise::CFG_APPRISE_URL,
            "Optional Apprise API URL for core notifications, e.g. online status changes. Empty disables notifications.",
            pm_config::OPT_TYPE_STRING,
            Value::String(String::new()),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(515))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;

    config.register(
        ConfigOption::new(
            "Release Channel",
            CFG_RELEASE_CHANNEL,
            r#"Use "Stable" for the best experience. The "Beta" channel will have the newest features and fixes, but may also break and cause interruption. Use others only temporarily and when instructed."#,
            pm_config::OPT_TYPE_STRING,
            Value::String("stable".to_string()),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_EXPERT)
        .with_release_level(pm_config::RELEASE_LEVEL_STABLE)
        .with_requires_restart(true)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(-4))
        .with_annotation(
            pm_config::DISPLAY_HINT_ANNOTATION,
            json!(pm_config::DISPLAY_HINT_ONE_OF),
        )
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Updates"))
        .with_possible_values(vec![
            PossibleValue {
                name: "Stable".to_string(),
                description: "Production releases.".to_string(),
                value: Value::String("stable".to_string()),
            },
            PossibleValue {
                name: "Beta".to_string(),
                description: "Production releases for testing new features that may break and cause interruption.".to_string(),
                value: Value::String("beta".to_string()),
            },
            PossibleValue {
                name: "Support".to_string(),
                description: "Support releases or version changes for troubleshooting. Only use temporarily and when instructed.".to_string(),
                value: Value::String("support".to_string()),
            },
            PossibleValue {
                name: "Staging".to_string(),
                description: "Dangerous development releases for testing random things and experimenting. Only use temporarily and when instructed.".to_string(),
                value: Value::String("staging".to_string()),
            },
        ]),
    )?;

    config.register(
        ConfigOption::new(
            "Automatic Software Updates",
            CFG_AUTOMATIC_UPDATES,
            "Automatically check for and download software updates. This does not include intelligence data updates.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_EXPERT)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(-12))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Updates")),
    )?;

    config.register(
        ConfigOption::new(
            "Automatic Intelligence Data Updates",
            CFG_AUTOMATIC_INTEL_UPDATES,
            "Automatically check for and download intelligence data updates. This includes filter lists, geo-ip data, and more. Does not include software updates.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(true),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_EXPERT)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(-11))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Updates")),
    )?;

    config.register(
        ConfigOption::new(
            "Development Mode",
            CFG_DEV_MODE,
            "In Development Mode, security restrictions are lifted/softened to enable unrestricted access for debugging and testing purposes.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(options.dev_mode),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(512))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;

    config.register(
        ConfigOption::new(
            "Log Level",
            CFG_LOG_LEVEL,
            "Configure the logging level.",
            pm_config::OPT_TYPE_STRING,
            Value::String(options.log_level.clone()),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(513))
        .with_annotation(
            pm_config::DISPLAY_HINT_ANNOTATION,
            json!(pm_config::DISPLAY_HINT_ONE_OF),
        )
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development"))
        .with_possible_values(
            ["critical", "error", "warning", "info", "debug", "trace"]
                .iter()
                .map(|level| PossibleValue {
                    name: (*level).to_string(),
                    description: String::new(),
                    value: Value::String((*level).to_string()),
                })
                .collect(),
        ),
    )?;

    config.register(
        ConfigOption::new(
            "API Listen Address",
            CFG_LISTEN_ADDRESS,
            "Defines the IP address and port on which the internal API listens.",
            pm_config::OPT_TYPE_STRING,
            Value::String(options.api_address.clone()),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_requires_restart(true)
        .with_validation_regex(
            "^([0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}:[0-9]{1,5}|\\[[:0-9A-Fa-f]+\\]:[0-9]{1,5})$",
        )
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(513))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;

    config.register(
        ConfigOption::new(
            "API Keys",
            CFG_API_KEYS,
            "Define API keys for privileged access to the API. Every entry is a separate API key with respective permissions. Format is `<key>?read=<perm>&write=<perm>`. Permissions are `anyone`, `user` and `admin`, and may be omitted.",
            pm_config::OPT_TYPE_STRING_ARRAY,
            json!([]),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::DISPLAY_ORDER_ANNOTATION, json!(514))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;

    Ok(())
}

/// Registers the update trigger endpoints.
fn register_update_endpoints(
    api: &Api,
    binary_updater: Arc<pm_updates::Updater>,
    intel_updater: Arc<pm_updates::Updater>,
    lists: Arc<pm_intel::FilterLists>,
    module: Arc<pm_intel::FilterListsModule>,
    geoip: Arc<pm_geoip::GeoIp>,
    archives: Option<Arc<UiArchives>>,
) -> anyhow::Result<()> {
    let check_binary = Arc::clone(&binary_updater);
    let check_intel = Arc::clone(&intel_updater);
    let check_lists = Arc::clone(&lists);
    let check_module = Arc::clone(&module);
    let check_geoip = Arc::clone(&geoip);
    let check_archives = archives.clone();
    api.register_endpoint(
        pm_api::Endpoint::new(
            "updates/check",
            "Trigger updates check event",
            "",
        )
        .with_write(pm_api::PERMIT_USER)
        .with_write_method("POST")
        .with_action(move |_| {
            let binary_updater = Arc::clone(&check_binary);
            let intel_updater = Arc::clone(&check_intel);
            let lists = Arc::clone(&check_lists);
            let module = Arc::clone(&check_module);
            let geoip = Arc::clone(&check_geoip);
            let archives = check_archives.clone();
            tokio::task::spawn_blocking(move || {
                if let Some(archives) = &archives {
                    archives.enable_upgrade_lock();
                }
                if let Err(error) = binary_updater.update_check() {
                    tracing::warn!(%error, "updates/binaries: update check failed");
                }
                let _ = module.handle_resources_updated(|| {
                    intel_updater
                        .update_check()
                        .map_err(|error| error.to_string())?;
                    apply_intel_update(&intel_updater, &lists, &geoip)
                });
                if let Some(archives) = &archives {
                    archives.disable_upgrade_lock();
                }
            });
            Ok("update check triggered".to_string())
        }),
    )
    .map_err(|error| anyhow::anyhow!("register updates/check: {error}"))?;

    let get_binary = Arc::clone(&binary_updater);
    let get_intel = Arc::clone(&intel_updater);
    api.register_endpoint(
        pm_api::Endpoint::new(
            "updates/get/{artifact_path}/{artifact_name}",
            "Get Resource",
            "Returns the requested resource from the update system",
        )
        .with_read(pm_api::PERMIT_USER)
        .with_data(move |request| {
            let name = request.param("artifact_name").unwrap_or_default();
            if name.is_empty() {
                return Err(pm_api::ApiError::Message("no resource specified".to_string()).with_status(400));
            }
            let artifact = get_binary.get_file(name)
                .or_else(|_| get_intel.get_file(name))
                .map_err(|error| pm_api::ApiError::Message(error.to_string()).with_status(404))?;
            std::fs::read(artifact.path())
                .map_err(|error| pm_api::ApiError::Message(error.to_string()).with_status(500))
        }),
    )
    .map_err(|error| anyhow::anyhow!("register updates/get: {error}"))?;

    api.register_endpoint(
        pm_api::Endpoint::new("updates/apply", "Trigger updates apply event", "")
            .with_write(pm_api::PERMIT_USER)
            .with_write_method("POST")
            .with_action(move |_| {
                let binary_updater = Arc::clone(&binary_updater);
                let intel_updater = Arc::clone(&intel_updater);
                let lists = Arc::clone(&lists);
                let module = Arc::clone(&module);
                let geoip = Arc::clone(&geoip);
                let archives = archives.clone();
                tokio::task::spawn_blocking(move || {
                    if let Some(archives) = &archives {
                        archives.enable_upgrade_lock();
                    }
                    if let Err(error) = binary_updater.upgrade() {
                        tracing::warn!(%error, "updates/binaries: upgrade failed");
                    }
                    let _ = module.handle_resources_updated(|| {
                        intel_updater
                            .upgrade()
                            .map_err(|error| error.to_string())?;
                        apply_intel_update(&intel_updater, &lists, &geoip)
                    });
                    if let Some(archives) = &archives {
                        archives.disable_upgrade_lock();
                    }
                });
                Ok("upgrade triggered".to_string())
            }),
    )
    .map_err(|error| anyhow::anyhow!("register updates/apply: {error}"))?;

    Ok(())
}

/// Applies the filter list files of the current intel index, like Go
/// `performUpdate` triggered by the resource update event.
fn apply_intel_update(
    updater: &pm_updates::Updater,
    lists: &pm_intel::FilterLists,
    geoip: &pm_geoip::GeoIp,
) -> std::result::Result<(), String> {
    let index_file = updater
        .get_file(pm_intel::LIST_INDEX_FILE)
        .map_err(|error| format!("list index not found: {error}"))?;
    let candidates: Vec<pm_updates::Artifact> = pm_intel::LIST_FILES
        .iter()
        .filter_map(|filename| updater.get_file(filename).ok())
        .collect();
    if candidates.is_empty() {
        return Ok(());
    }
    pm_intel::update_filter_lists(
        lists,
        lists.cache(),
        index_file.path(),
        &index_file.version,
        candidates,
        pm_database::now(),
    )
    .map_err(|error| error.to_string())?;

    // Reload the GeoIP databases of the new intel index, like the Go geoip
    // worker reacting to the resource update event.
    for resource in [pm_geoip::V4_RESOURCE, pm_geoip::V6_RESOURCE] {
        if let Ok(file) = updater.get_file(resource) {
            if let Err(error) = geoip.replace_resource(file.path(), resource) {
                tracing::warn!(%error, resource, "intel/geoip: failed to load database");
            }
        }
    }

    Ok(())
}

/// Waits for a shutdown signal (SIGINT/SIGTERM).
async fn shutdown_signal() {
    let ctrl_c = async {
        let _ = tokio::signal::ctrl_c().await;
    };

    #[cfg(unix)]
    let terminate = async {
        match tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()) {
            Ok(mut signal) => {
                signal.recv().await;
            }
            Err(_) => std::future::pending::<()>().await,
        }
    };
    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {}
        _ = terminate => {}
    }
}

/// Runs database maintenance until the service stops.
async fn maintenance(databases: Arc<Databases>) {
    let mut interval = tokio::time::interval(Duration::from_secs(5 * 60));
    loop {
        interval.tick().await;
        if let Err(error) = databases.maintain_record_states() {
            tracing::warn!(%error, "database maintenance failed");
        }
        if let Err(error) = databases.maintain() {
            tracing::warn!(%error, "database vacuum failed");
        }
    }
}
