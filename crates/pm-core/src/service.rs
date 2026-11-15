//! Service startup: databases, configuration and the API.

use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_api::{Api, UiArchives};
use pm_config::{Config, ConfigOption, PossibleValue};
use pm_database::{DatabaseInfo, Databases};
use serde_json::{json, Value};
use tokio::net::TcpListener;

use crate::cli::Options;

/// Configuration keys of the core.
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

        // Filter lists live in the cache database.
        let filter_lists = Arc::new(pm_intel::FilterLists::new(pm_intel::cache_interface(
            &databases,
        )));

        // Configuration.
        let config = Config::new();
        register_options(&config, options)?;
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

        let listener = TcpListener::bind(&listen_address)
            .await
            .map_err(|error| anyhow::anyhow!("failed to listen on {listen_address}: {error}"))?;
        tracing::info!(address = %listener.local_addr()?, "api: listening");

        api.set_ready(true);

        Ok(Service {
            config,
            databases,
            api,
            runtime,
            notifications,
            status,
            filter_lists,
            local_addr: listener.local_addr()?,
            listener: Mutex::new(Some(listener)),
            data_dir,
        })
    }

    /// Returns the bound API address.
    pub fn local_addr(&self) -> std::net::SocketAddr {
        self.local_addr
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

        tokio::select! {
            result = server => result?,
            _ = shutdown_signal() => {
                tracing::info!("shutting down");
            }
            _ = maintenance(Arc::clone(&databases)) => {}
        }

        databases.shutdown()?;
        Ok(())
    }

    /// Shuts the service down.
    pub fn shutdown(&self) -> anyhow::Result<()> {
        self.databases.shutdown()?;
        Ok(())
    }
}

fn register_options(config: &Config, options: &Options) -> anyhow::Result<()> {
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
