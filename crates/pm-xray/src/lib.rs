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
    UpdateSubscriptionRequest, XrayNode, XrayStatus,
};
pub use stats::{NoopTrafficStats, TrafficSnapshot, TrafficStats};
pub use subscription::{HttpFetcher, SubscriptionFetcher, SubscriptionStore, XRAY_DB};

/// Path of the Xray binary in the configuration.
pub const CFG_XRAY_BINARY: &str = "core/xrayBinary";
/// Whether the Xray integration is enabled.
pub const CFG_XRAY_ENABLE: &str = "xray/enable";
/// Local SOCKS/HTTP port exposed by Xray.
pub const CFG_XRAY_INBOUND_PORT: &str = "xray/inboundPort";
/// Local inbound protocol: `socks` or `http`.
pub const CFG_XRAY_INBOUND_PROTOCOL: &str = "xray/inboundProtocol";
/// Binary name looked up in the bin directory and PATH.
pub const XRAY_BINARY_NAME: &str = "xray-rust";

/// Registers the Xray configuration options.
pub fn register_options(config: &Config) -> pm_config::Result<()> {
    config.register(
        ConfigOption::new(
            "Xray Binary",
            CFG_XRAY_BINARY,
            "Path to the xray-rust binary. Empty uses the binary directory or PATH.",
            pm_config::OPT_TYPE_STRING,
            Value::String(String::new()),
        )
        .with_expertise_level(pm_config::EXPERTISE_LEVEL_DEVELOPER)
        .with_annotation(pm_config::CATEGORY_ANNOTATION, json!("Development")),
    )?;
    config.register(
        ConfigOption::new(
            "Xray Integration",
            CFG_XRAY_ENABLE,
            "Enable the Xray VPN integration. Subscriptions and nodes can still be managed when disabled, but no process is started.",
            pm_config::OPT_TYPE_BOOL,
            Value::Bool(false),
        )
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
    )
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
    let candidate = bin_dir.join(XRAY_BINARY_NAME);
    if candidate.is_file() {
        return Some(candidate.to_string_lossy().into_owned());
    }
    exists_in_path(XRAY_BINARY_NAME).then(|| XRAY_BINARY_NAME.to_string())
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
    /// Placeholder; a real gRPC StatsService adapter can replace it.
    pub stats: NoopTrafficStats,
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
        let configured_binary = config.get_as_string(CFG_XRAY_BINARY, "")();
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

        Arc::new(Xray {
            config: Arc::clone(config),
            store: SubscriptionStore::new(databases)
                .expect("register xray database"),
            manager: XrayManager::system(binary, data_dir),
            geoip,
            enabled,
            inbound_port,
            inbound,
            stats: NoopTrafficStats,
        })
    }

    /// Creates a service with an injectable process adapter, for tests.
    pub fn with_adapter(
        config: &Arc<Config>,
        databases: &Arc<Databases>,
        geoip: Arc<GeoIp>,
        data_dir: PathBuf,
        binary: String,
        adapter: Box<dyn ProcessAdapter>,
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
            stats: NoopTrafficStats,
        })
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
