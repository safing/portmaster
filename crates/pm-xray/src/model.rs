//! Data model of the Xray integration.
//!
//! A *subscription* is a URL that returns a list of proxy profiles (usually a
//! Base64-encoded list of `vless://`, `vmess://`, `trojan://`, `ss://`,
//! `socks://` or `https://` links, plus `hysteria2://`/`hy2://` and
//! `wireguard://`). Every profile becomes an [`XrayNode`] that the UI can
//! place on the existing SPN map.

use serde::{Deserialize, Serialize};

/// Proxy protocol of a parsed profile.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, Default)]
#[serde(rename_all = "lowercase")]
pub enum Protocol {
    Vless,
    Vmess,
    Trojan,
    #[serde(rename = "shadowsocks", alias = "ss")]
    Shadowsocks,
    #[serde(rename = "socks", alias = "socks5")]
    Socks,
    #[serde(rename = "http", alias = "https")]
    Http,
    /// Hysteria 2 (`hysteria://`, `hysteria2://`, `hy2://`). Xray's outbound
    /// protocol name is `hysteria`, version 2.
    #[serde(rename = "hysteria", alias = "hysteria2", alias = "hy2")]
    Hysteria2,
    Wireguard,
    #[default]
    Unknown,
}

impl Protocol {
    /// Stable lowercase name, also used as the Xray outbound protocol.
    pub fn as_str(&self) -> &'static str {
        match self {
            Protocol::Vless => "vless",
            Protocol::Vmess => "vmess",
            Protocol::Trojan => "trojan",
            Protocol::Shadowsocks => "shadowsocks",
            Protocol::Socks => "socks",
            Protocol::Http => "http",
            Protocol::Hysteria2 => "hysteria",
            Protocol::Wireguard => "wireguard",
            Protocol::Unknown => "unknown",
        }
    }

    /// Parses a protocol name, accepting the common URI spellings.
    pub fn parse(text: &str) -> Protocol {
        match text.trim().to_ascii_lowercase().as_str() {
            "vless" => Protocol::Vless,
            "vmess" => Protocol::Vmess,
            "trojan" => Protocol::Trojan,
            "ss" | "ssr" | "shadowsocks" => Protocol::Shadowsocks,
            "socks" | "socks5" | "socks5h" => Protocol::Socks,
            "http" | "https" => Protocol::Http,
            "hysteria" | "hysteria2" | "hy2" => Protocol::Hysteria2,
            "wireguard" | "wg" => Protocol::Wireguard,
            _ => Protocol::Unknown,
        }
    }
}

impl std::fmt::Display for Protocol {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// One VPN endpoint extracted from a subscription.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct XrayNode {
    /// Stable identifier: a short hash of the subscription and share link.
    pub id: String,
    /// ID of the subscription this node came from.
    #[serde(rename = "subscriptionId", alias = "subscription_id")]
    pub subscription_id: String,
    /// Human-readable name from the profile fragment/link.
    pub name: String,
    /// ISO 3166-1 alpha-2 country code, when known. The UI calls this
    /// simply `country`.
    #[serde(rename = "country", alias = "country_code", default)]
    pub country_code: String,
    /// Human-readable country name, when known.
    #[serde(default)]
    pub country_name: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub latitude: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub longitude: Option<f64>,
    pub protocol: Protocol,
    pub address: String,
    pub port: u16,
    /// Original share link. May contain credentials; never log it verbatim.
    pub raw_config: String,
}

/// A subscription, as stored in the `xray` database.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Subscription {
    pub id: String,
    pub name: String,
    pub url: String,
    pub enabled: bool,
    /// Unix timestamp of the last successful refresh. The UI expects this
    /// under `added_at`.
    #[serde(rename = "added_at", alias = "last_updated", default)]
    pub last_updated: i64,
    /// Number of nodes from the last refresh.
    #[serde(default)]
    pub node_count: usize,
}

/// Payload of `POST xray/subscriptions`.
#[derive(Debug, Clone, Deserialize)]
pub struct AddSubscriptionRequest {
    /// Optional: the UI only sends the URL.
    #[serde(default)]
    pub name: String,
    pub url: String,
    #[serde(default = "default_enabled")]
    pub enabled: bool,
}

fn default_enabled() -> bool {
    true
}

/// Payload of `POST xray/subscriptions/update`.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct UpdateSubscriptionRequest {
    /// When empty, every enabled subscription is refreshed.
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub url: Option<String>,
    #[serde(default)]
    pub enabled: Option<bool>,
}

/// Payload of `POST xray/connect`.
#[derive(Debug, Clone, Deserialize)]
pub struct ConnectRequest {
    /// The UI sends `nodeId` (and also an unused `id` for compatibility).
    #[serde(alias = "nodeId")]
    pub node_id: String,
    /// Local SOCKS/HTTP port; falls back to the configured port when absent.
    #[serde(default)]
    pub inbound_port: Option<u16>,
}

/// Lifecycle state of the Xray child process.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "lowercase")]
pub enum ProcessState {
    #[default]
    Stopped,
    Starting,
    Running,
    Failed,
}

impl ProcessState {
    pub fn as_str(&self) -> &'static str {
        match self {
            ProcessState::Stopped => "stopped",
            ProcessState::Starting => "starting",
            ProcessState::Running => "running",
            ProcessState::Failed => "failed",
        }
    }
}

/// Traffic counters of the selected node, read from Xray's StatsService.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize)]
pub struct TrafficStatus {
    /// Bytes sent through the proxy outbound.
    pub uplink: u64,
    /// Bytes received through the proxy outbound.
    pub downlink: u64,
}

/// Response of `GET xray/status`.
#[derive(Debug, Clone, Serialize)]
pub struct XrayStatus {
    pub enabled: bool,
    /// Configured or resolved binary path.
    pub binary: String,
    pub binary_available: bool,
    pub state: ProcessState,
    /// UI-facing alias of `node_id`.
    #[serde(rename = "nodeId", alias = "node_id", skip_serializing_if = "Option::is_none")]
    pub node_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub node_name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub inbound_address: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    /// Convenience fields consumed by the Angular UI.
    pub connected: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub country: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub since: Option<String>,
    /// Live traffic counters, when a StatsService API is configured.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub traffic: Option<TrafficStatus>,
    /// Real detected exit node IP (for cascading proxies or CDN frontends).
    #[serde(rename = "exitIp", alias = "exit_ip", skip_serializing_if = "Option::is_none")]
    pub exit_ip: Option<String>,
    #[serde(rename = "exitCountry", alias = "exit_country", skip_serializing_if = "Option::is_none")]
    pub exit_country: Option<String>,
    #[serde(rename = "exitLatitude", alias = "exit_latitude", skip_serializing_if = "Option::is_none")]
    pub exit_latitude: Option<f64>,
    #[serde(rename = "exitLongitude", alias = "exit_longitude", skip_serializing_if = "Option::is_none")]
    pub exit_longitude: Option<f64>,
}

/// Response of `GET xray/nodes`.
#[derive(Debug, Serialize)]
pub struct NodesResponse {
    pub nodes: Vec<XrayNode>,
}
