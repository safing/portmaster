//! Xray integration for the Portmaster core.
//!
//! The Rust Xray core lives in a separate, MPL-2.0 licensed workspace and is
//! therefore **not** linked into this GPL-3.0 crate. Instead the `xray-rust`
//! binary is launched as a child process:
//!
//! ```text
//! xray-rust run -config <config.json>
//! ```
//!
//! The crate stores subscriptions in `pm-database`, fetches and parses their
//! profiles into [`model::XrayNode`]s (including countries for the SPN map),
//! generates an Xray JSON config, manages the child process and exposes a REST
//! API through `pm-api`.

pub mod api;
pub mod config;
pub mod error;
pub mod manager;
pub mod model;
pub mod parser;
pub mod stats;
pub mod subscription;

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use pm_config::{Config, ConfigOption};
use pm_database::Databases;
use pm_geoip::GeoIp;
use serde_json::{json, Value};

pub use config::{InboundProtocol, DEFAULT_INBOUND_PORT};
pub use error::{Error, Result};
pub use manager::{ChildHandle, ProcessAdapter, SystemProcessAdapter, XrayManager};
pub use model::{
    AddSubscriptionRequest, ConnectRequest, NodesResponse, ProcessState, Protocol, Subscription,
    TrafficStatus, UpdateSubscriptionRequest, XrayNode, XrayStatus,
};
pub use stats::{
    GrpcResponse, GrpcStats, GrpcStatus, GrpcTransport, H2Transport, NoopTrafficStats, StatsError,
    StatsService, TrafficCounter, TrafficSnapshot, TrafficStats, DEFAULT_STATS_PORT, GET_STATS_PATH,
    QUERY_STATS_PATH,
};
pub use subscription::{HttpFetcher, SubscriptionFetcher, SubscriptionStore, XRAY_DB};

/// Path of the Xray binary in the configuration.
pub const CFG_XRAY_BINARY: &str = "core/xrayBinary";
/// Whether the Xray integration is enabled.
pub const CFG_XRAY_ENABLE: &str = "xray/enable";
/// Local SOCKS/HTTP port exposed by Xray.
pub const CFG_XRAY_INBOUND_PORT: &str = "xray/inboundPort";
/// Local inbound protocol: `socks` or `http`.
pub const CFG_XRAY_INBOUND_PROTOCOL: &str = "xray/inboundProtocol";
/// Port of the local gRPC API inbound exposing Xray's StatsService. 0 disables it.
pub const CFG_XRAY_STATS_PORT: &str = "xray/statsPort";
/// Primary Xray binary setting under xray namespace.
pub const CFG_XRAY_BINARY_NAMESPACED: &str = "xray/binary";
/// Binary name looked up in the bin directory and PATH.
pub const XRAY_BINARY_NAME: &str = "xray-rust";

fn xray_binary_options() -> Vec<pm_config::PossibleValue> {
    vec![
        pm_config::PossibleValue {
            name: "Auto-detect (поиск: <папка Portmaster>/xray-rust, /usr/bin, /usr/local/bin, /opt/xray-rust, PATH)".to_string(),
            description: "Автоматический поиск бинарника xray-rust в каталоге Portmaster, стандартных путях и PATH".to_string(),
            value: Value::String(String::new()),
        },
        pm_config::PossibleValue {
            name: "xray-rust (/usr/bin/xray-rust, /usr/local/bin/xray-rust, /opt/xray-rust/xray-rust)".to_string(),
            description: "Выделенный бинарник xray-rust".to_string(),
            value: Value::String("xray-rust".to_string()),
        },
        pm_config::PossibleValue {
            name: "v2rayN Binary (/opt/v2rayn-bin/bin/xray/xray)".to_string(),
            description: "Встроенный бинарник Xray core из v2rayN (/opt/v2rayn-bin/bin/xray/xray)".to_string(),
            value: Value::String("/opt/v2rayn-bin/bin/xray/xray".to_string()),
        },
        pm_config::PossibleValue {
            name: "System binary (/usr/bin/xray)".to_string(),
            description: "Системный бинарник /usr/bin/xray".to_string(),
            value: Value::String("/usr/bin/xray".to_string()),
        },
        pm_config::PossibleValue {
            name: "Local binary (/usr/local/bin/xray)".to_string(),
            description: "Локальный бинарник /usr/local/bin/xray".to_string(),
            value: Value::String("/usr/local/bin/xray".to_string()),
        },
        pm_config::PossibleValue {
            name: "xray (PATH)".to_string(),
            description: "Стандартный бинарник xray из системного PATH".to_string(),
            value: Value::String("xray".to_string()),
        },
    ]
}

/// Registers the Xray configuration options.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    let default_binary = if Path::new("/opt/v2rayn-bin/bin/xray/xray").is_file() {
        "/opt/v2rayn-bin/bin/xray/xray".to_string()
    } else {
        String::new()
    };

    config.register(
        ConfigOption::new(
            "Xray Binary",
            CFG_XRAY_BINARY,
            "Путь к бинарнику Xray. Автопоиск проверяет: <папка Portmaster>/xray-rust, /usr/bin/xray-rust, /usr/local/bin/xray-rust, /opt/xray-rust/xray-rust, /opt/v2rayn-bin/bin/xray/xray или PATH.",
            pm_config::OPT_TYPE_STRING,
            Value::String(default_binary.clone()),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_USER)
        .with_possible_values(xray_binary_options())
        .with_annotation(pm_config::DISPLAY_HINT_ANNOTATION, json!(pm_config::DISPLAY_HINT_ONE_OF))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General")),
    )?;
    config.register(
        ConfigOption::new(
            "Xray Binary",
            CFG_XRAY_BINARY_NAMESPACED,
            "Path to the Xray binary. Select a pre-installed binary or auto-detect.",
            pm_config::OPT_TYPE_STRING,
            Value::String(default_binary),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_USER)
        .with_possible_values(xray_binary_options())
        .with_annotation(pm_config::DISPLAY_HINT_ANNOTATION, json!(pm_config::DISPLAY_HINT_ONE_OF))
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General")),
    )?;
    config.register(
        ConfigOption::new(
            "Xray Integration",
            CFG_XRAY_ENABLE,
            "Enable the Xray VPN integration. Subscriptions and nodes can still be managed when disabled, but no process is started.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(false),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_USER)
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General")),
    )?;
    config.register(
        ConfigOption::new(
            "Xray Local Port",
            CFG_XRAY_INBOUND_PORT,
            "Port of the local SOCKS/HTTP proxy exposed by Xray.",
            pm_config::OPT_TYPE_INT,
            Value::from(DEFAULT_INBOUND_PORT),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;
    config.register(
        ConfigOption::new(
            "Xray Local Protocol",
            CFG_XRAY_INBOUND_PROTOCOL,
            "Local inbound protocol used by Xray: socks or http.",
            pm_config::OPT_TYPE_STRING,
            Value::String("socks".to_string()),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;
    config.register(
        ConfigOption::new(
            "Xray Stats Port",
            CFG_XRAY_STATS_PORT,
            "Port of the local gRPC API inbound exposing Xray's StatsService. Set to 0 to disable traffic statistics.",
            pm_config::OPT_TYPE_INT,
            Value::from(i64::from(DEFAULT_STATS_PORT)),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;

    if config.option("spn/enable").is_err() {
        let _ = config.register(
            ConfigOption::new(
                "Enable SPN / Xray Routing",
                "spn/enable",
                "Whether SPN / Xray routing is enabled globally.",
                pm_config::OPT_TYPE_BOOL,
                Value::Bool(false),
            )
            .with_expertise_level(pm_config::EXPERTISE_LEVEL_USER)
            .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("General")),
        );
    }

    Ok(())
}

fn exists_in_path(name: &str) -> bool {
    std::env::var_os("PATH")
        .map(|paths| {
            std::env::split_paths(&paths)
                .any(|directory| directory.join(name).is_file())
        })
        .unwrap_or(false)
}

/// Resolves the binary path: explicit config, then the bin directory, then
/// PATH. Returns `None` when nothing is found so callers can warn instead of
/// failing to start the core.
fn resolve_binary(configured: &str, bin_dir: &Path) -> Option<String> {
    if !configured.is_empty() {
        let path = Path::new(configured);
        if path.is_absolute() || configured.contains('/') || configured.contains('\\') {
            return path.is_file().then(|| configured.to_string());
        }
        return exists_in_path(configured).then(|| configured.to_string());
    }
    const KNOWN_PATHS: &[&str] = &[
        "/opt/v2rayn-bin/bin/xray/xray",
        "/usr/bin/xray",
        "/usr/local/bin/xray",
        "/opt/xray-rust/xray-rust",
        "/usr/bin/xray-rust",
        "/usr/local/bin/xray-rust",
    ];
    for &known in KNOWN_PATHS {
        if Path::new(known).is_file() {
            return Some(known.to_string());
        }
    }
    let candidate = bin_dir.join(XRAY_BINARY_NAME);
    if candidate.is_file() {
        return Some(candidate.to_string_lossy().into_owned());
    }
    let candidate_xray = bin_dir.join("xray");
    if candidate_xray.is_file() {
        return Some(candidate_xray.to_string_lossy().into_owned());
    }
    #[cfg(windows)]
    {
        let candidate_exe = bin_dir.join(format!("{XRAY_BINARY_NAME}.exe"));
        if candidate_exe.is_file() {
            return Some(candidate_exe.to_string_lossy().into_owned());
        }
        let candidate_xray_exe = bin_dir.join("xray.exe");
        if candidate_xray_exe.is_file() {
            return Some(candidate_xray_exe.to_string_lossy().into_owned());
        }
    }
    if exists_in_path("xray") {
        return Some("xray".to_string());
    }
    if exists_in_path(XRAY_BINARY_NAME) {
        return Some(XRAY_BINARY_NAME.to_string());
    }
    #[cfg(windows)]
    {
        if exists_in_path("xray.exe") {
            return Some("xray.exe".to_string());
        }
        if exists_in_path(&format!("{XRAY_BINARY_NAME}.exe")) {
            return Some(format!("{XRAY_BINARY_NAME}.exe"));
        }
    }
    None
}

/// Current time as Unix seconds.
pub fn now() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs() as i64)
        .unwrap_or(0)
}

/// The Xray integration service, owned by the core.
pub struct Xray {
    pub config: Arc<Config>,
    pub store: SubscriptionStore,
    pub manager: XrayManager,
    pub geoip: Arc<GeoIp>,
    pub enabled: bool,
    pub inbound_port: u16,
    pub inbound: InboundProtocol,
    /// Source of traffic statistics: real gRPC client when a stats port is
    /// configured, [`NoopTrafficStats`] otherwise.
    pub stats: Arc<dyn StatsService>,
}

impl Xray {
    /// Creates the service. A missing or disabled binary is only a warning:
    /// construction never fails because of it.
    pub fn new(
        config: &Arc<Config>,
        databases: &Arc<Databases>,
        geoip: Arc<GeoIp>,
        data_dir: PathBuf,
        bin_dir: PathBuf,
    ) -> Arc<Xray> {
        let enabled = config.get_as_bool(CFG_XRAY_ENABLE, false)();
        let configured_binary = {
            let b = config.get_as_string(CFG_XRAY_BINARY_NAMESPACED, "")();
            if b.is_empty() {
                config.get_as_string(CFG_XRAY_BINARY, "")()
            } else {
                b
            }
        };
        let binary = resolve_binary(&configured_binary, &bin_dir).unwrap_or_default();
        let inbound_port = config
            .get_as_int(CFG_XRAY_INBOUND_PORT, i64::from(DEFAULT_INBOUND_PORT))()
            .clamp(1, u16::MAX as i64) as u16;
        let inbound = if config
            .get_as_string(CFG_XRAY_INBOUND_PROTOCOL, "socks")()
            .eq_ignore_ascii_case("http")
        {
            InboundProtocol::Http
        } else {
            InboundProtocol::Socks
        };
        let stats_port = config
            .get_as_int(CFG_XRAY_STATS_PORT, i64::from(DEFAULT_STATS_PORT))()
            .clamp(0, u16::MAX as i64) as u16;
        let stats: Arc<dyn StatsService> = if stats_port == 0 {
            Arc::new(NoopTrafficStats)
        } else {
            Arc::new(GrpcStats::new(stats_port))
        };

        Arc::new(Xray {
            config: Arc::clone(config),
            store: SubscriptionStore::new(databases)
                .expect("register xray database"),
            manager: XrayManager::system_with_stats_port(binary, data_dir, stats_port),
            geoip,
            enabled,
            inbound_port,
            inbound,
            stats,
        })
    }

    /// Creates a service with an injectable process adapter, for tests.
    /// Stats fall back to the no-op source unless [`Xray::with_adapter_and_stats`] is used.
    pub fn with_adapter(
        config: &Arc<Config>,
        databases: &Arc<Databases>,
        geoip: Arc<GeoIp>,
        data_dir: PathBuf,
        binary: String,
        adapter: Box<dyn ProcessAdapter>,
    ) -> Arc<Xray> {
        Self::with_adapter_and_stats(
            config,
            databases,
            geoip,
            data_dir,
            binary,
            adapter,
            Arc::new(NoopTrafficStats),
        )
    }

    /// Like [`Xray::with_adapter`], with an injectable traffic stats source.
    pub fn with_adapter_and_stats(
        config: &Arc<Config>,
        databases: &Arc<Databases>,
        geoip: Arc<GeoIp>,
        data_dir: PathBuf,
        binary: String,
        adapter: Box<dyn ProcessAdapter>,
        stats: Arc<dyn StatsService>,
    ) -> Arc<Xray> {
        Arc::new(Xray {
            config: Arc::clone(config),
            store: SubscriptionStore::new(databases)
                .expect("register xray database"),
            manager: XrayManager::new(binary, data_dir, adapter),
            geoip,
            enabled: true,
            inbound_port: DEFAULT_INBOUND_PORT,
            inbound: InboundProtocol::Socks,
            stats,
        })
    }

    /// Returns the process state enriched with live traffic counters. Stats
    /// are only queried for a running process; failures are logged and leave
    /// the `traffic` field unset so the status endpoint stays usable.
    pub fn status(&self) -> XrayStatus {
        let mut status = self.manager.status();
        if status.connected && self.stats.available() {
            match self.stats.traffic("proxy") {
                Ok(snapshot) => {
                    status.traffic = Some(TrafficStatus {
                        uplink: snapshot.uplink,
                        downlink: snapshot.downlink,
                    });
                }
                Err(error) => tracing::warn!(%error, "xray: stats unavailable"),
            }
        }
        status
    }

    /// Refreshes subscriptions and returns the number of discovered nodes.
    /// Fetch errors are logged and reported as zero; they never abort startup.
    pub fn refresh(&self, id: &str, fetcher: Option<&dyn SubscriptionFetcher>) -> usize {
        let fallback = HttpFetcher::new();
        let fetcher = fetcher.unwrap_or(&fallback);
        match self.store.refresh(id, fetcher, Some(&self.geoip)) {
            Ok(count) => count,
            Err(error) => {
                tracing::warn!(%error, "xray: subscription refresh failed");
                0
            }
        }
    }

    /// Starts the service: restores the last selected node when one exists.
    pub fn start(&self) {
        if !self.enabled {
            tracing::info!("xray: integration is disabled");
            return;
        }
        if !self.manager.binary_available() {
            tracing::warn!(
                binary = XRAY_BINARY_NAME,
                "xray: binary not found; integration stays inactive"
            );
            return;
        }
        let Some(node_id) = self.store.active_node_id() else {
            return;
        };
        match self.store.get_node(&node_id) {
            Ok(node) => {
                if let Err(error) = self.manager.connect(&node, self.inbound_port, self.inbound) {
                    tracing::warn!(%error, "xray: failed to restore selected node");
                }
            }
            Err(error) => tracing::warn!(%error, "xray: stored active node is gone"),
        }
    }

    /// Stops the child process; called when the core shuts down.
    pub fn stop(&self) {
        self.manager.stop();
    }
}
