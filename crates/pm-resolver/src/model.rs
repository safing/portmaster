use std::fmt;
use std::net::IpAddr;
use std::sync::Arc;

use serde::{Deserialize, Deserializer, Serialize, Serializer};

use crate::block::is_blocked_upstream;
use crate::conn::ResolverConn;

// DNS Resolver Attributes.
pub const SERVER_TYPE_DNS: &str = "dns";
pub const SERVER_TYPE_TCP: &str = "tcp";
pub const SERVER_TYPE_DOT: &str = "dot";
pub const SERVER_TYPE_DOH: &str = "doh";
pub const SERVER_TYPE_MDNS: &str = "mdns";
pub const SERVER_TYPE_ENV: &str = "env";
pub const SERVER_TYPE_MONITOR: &str = "monitor";
pub const SERVER_TYPE_FIREWALL: &str = "firewall";

pub const SERVER_SOURCE_CONFIGURED: &str = "config";
pub const SERVER_SOURCE_OPERATING_SYSTEM: &str = "system";
pub const SERVER_SOURCE_MDNS: &str = "mdns";
pub const SERVER_SOURCE_ENV: &str = "env";
pub const SERVER_SOURCE_ETW: &str = "etw";
pub const SERVER_SOURCE_SYSTEMD: &str = "systemd";
pub const SERVER_SOURCE_FIREWALL: &str = "firewall";

// DNS resolver scheme aliases.
pub const HTTPS_PROTOCOL: &str = "https";
pub const TLS_PROTOCOL: &str = "tls";

// Supported upstream block detections.
pub const BLOCK_DETECTION_REFUSED: &str = "refused";
pub const BLOCK_DETECTION_ZERO_IP: &str = "zeroip";
pub const BLOCK_DETECTION_EMPTY_ANSWER: &str = "empty";
pub const BLOCK_DETECTION_DISABLED: &str = "disabled";

/// Network scope of an IP address, matching Go `netutils.IPScope`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
#[repr(i8)]
pub enum IpScope {
    Invalid = -1,
    #[default]
    Undefined = 0,
    HostLocal = 1,
    LinkLocal = 2,
    SiteLocal = 3,
    Global = 4,
    LocalMulticast = 5,
    GlobalMulticast = 6,
}

impl IpScope {
    /// Returns whether the IP refers to the host itself.
    pub fn is_localhost(self) -> bool {
        self == Self::HostLocal
    }

    /// Returns true if the scope is site-local or link-local.
    pub fn is_lan(self) -> bool {
        matches!(self, Self::SiteLocal | Self::LinkLocal | Self::LocalMulticast)
    }

    /// Returns true if the scope is global.
    pub fn is_global(self) -> bool {
        matches!(self, Self::Global | Self::GlobalMulticast)
    }
}

impl Serialize for IpScope {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        serializer.serialize_i8(*self as i8)
    }
}

impl<'de> Deserialize<'de> for IpScope {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let v = i8::deserialize(deserializer)?;
        match v {
            -1 => Ok(Self::Invalid),
            0 => Ok(Self::Undefined),
            1 => Ok(Self::HostLocal),
            2 => Ok(Self::LinkLocal),
            3 => Ok(Self::SiteLocal),
            4 => Ok(Self::Global),
            5 => Ok(Self::LocalMulticast),
            6 => Ok(Self::GlobalMulticast),
            _ => Ok(Self::Invalid),
        }
    }
}

impl From<pm_netstate::IpScope> for IpScope {
    fn from(s: pm_netstate::IpScope) -> Self {
        match s {
            pm_netstate::IpScope::Invalid => Self::Invalid,
            pm_netstate::IpScope::Undefined => Self::Undefined,
            pm_netstate::IpScope::HostLocal => Self::HostLocal,
            pm_netstate::IpScope::LinkLocal => Self::LinkLocal,
            pm_netstate::IpScope::SiteLocal => Self::SiteLocal,
            pm_netstate::IpScope::Global => Self::Global,
            pm_netstate::IpScope::LocalMulticast => Self::LocalMulticast,
            pm_netstate::IpScope::GlobalMulticast => Self::GlobalMulticast,
        }
    }
}

impl From<IpScope> for pm_netstate::IpScope {
    fn from(s: IpScope) -> Self {
        match s {
            IpScope::Invalid => pm_netstate::IpScope::Invalid,
            IpScope::Undefined => pm_netstate::IpScope::Undefined,
            IpScope::HostLocal => pm_netstate::IpScope::HostLocal,
            IpScope::LinkLocal => pm_netstate::IpScope::LinkLocal,
            IpScope::SiteLocal => pm_netstate::IpScope::SiteLocal,
            IpScope::Global => pm_netstate::IpScope::Global,
            IpScope::LocalMulticast => pm_netstate::IpScope::LocalMulticast,
            IpScope::GlobalMulticast => pm_netstate::IpScope::GlobalMulticast,
        }
    }
}

/// Helper module to serialize Option<IpAddr> as string or empty string in Go JSON style.
mod optional_ip_serde {
    use std::net::IpAddr;
    use serde::{Deserialize, Deserializer, Serializer};

    pub fn serialize<S>(ip: &Option<IpAddr>, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        match ip {
            Some(addr) => serializer.serialize_str(&addr.to_string()),
            None => serializer.serialize_str(""),
        }
    }

    pub fn deserialize<'de, D>(deserializer: D) -> Result<Option<IpAddr>, D::Error>
    where
        D: Deserializer<'de>,
    {
        let opt: Option<String> = Option::deserialize(deserializer)?;
        match opt {
            Some(s) if !s.is_empty() => s.parse::<IpAddr>().map(Some).map_err(serde::de::Error::custom),
            _ => Ok(None),
        }
    }
}

/// ResolverInfo is a subset of resolver attributes attached to answers.
/// Fields mirror Go `ResolverInfo` in declaration order.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResolverInfo {
    #[serde(rename = "Name", default)]
    pub name: String,

    #[serde(rename = "Type", default)]
    pub r#type: String,

    #[serde(rename = "Source", default)]
    pub source: String,

    #[serde(rename = "IP", default, with = "optional_ip_serde")]
    pub ip: Option<IpAddr>,

    #[serde(rename = "Domain", default)]
    pub domain: String,

    #[serde(rename = "IPScope", default)]
    pub ip_scope: IpScope,

    #[serde(rename = "Port", default)]
    pub port: u16,
}

impl ResolverInfo {
    /// Returns the unique ID of the resolver.
    pub fn id(&self) -> String {
        match self.r#type.as_str() {
            SERVER_TYPE_MDNS => SERVER_TYPE_MDNS.to_string(),
            SERVER_TYPE_ENV => SERVER_TYPE_ENV.to_string(),
            SERVER_TYPE_DOH => format!("https://{}:{}#{}", self.domain, self.port, self.source),
            SERVER_TYPE_DOT => format!("dot://{}:{}#{}", self.domain, self.port, self.source),
            _ => {
                let ip_str = self.ip.map(|ip| ip.to_string()).unwrap_or_default();
                format!("{}://{}:{}#{}", self.r#type, ip_str, self.port, self.source)
            }
        }
    }

    /// DescriptiveName returns a human readable representation of the resolver.
    pub fn descriptive_name(&self) -> String {
        if self.r#type == SERVER_TYPE_MDNS {
            "MDNS".to_string()
        } else if self.r#type == SERVER_TYPE_ENV {
            "Portmaster Environment".to_string()
        } else if !self.name.is_empty() {
            format!("{} ({})", self.name, self.id())
        } else if !self.domain.is_empty() {
            format!("{} ({})", self.domain, self.id())
        } else if let Some(ip) = self.ip {
            format!("{} ({})", ip, self.id())
        } else {
            format!(" ({})", self.id())
        }
    }
}

impl fmt::Display for ResolverInfo {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.descriptive_name())
    }
}

/// Resolver holds information about an active resolver.
/// Fields mirror Go `Resolver` in declaration order.
#[derive(Clone, Serialize, Deserialize)]
pub struct Resolver {
    #[serde(rename = "ConfigURL")]
    pub config_url: String,

    #[serde(rename = "Info")]
    pub info: ResolverInfo,

    #[serde(rename = "ServerAddress")]
    pub server_address: String,

    #[serde(rename = "UpstreamBlockDetection")]
    pub upstream_block_detection: String,

    #[serde(rename = "Search")]
    pub search: Option<Vec<String>>,

    #[serde(rename = "SearchOnly")]
    pub search_only: bool,

    #[serde(rename = "Path")]
    pub path: String,

    #[serde(rename = "LinkLocalUnavailable")]
    pub link_local_unavailable: bool,

    #[serde(skip)]
    pub conn: Option<Arc<dyn ResolverConn>>,
}

impl Resolver {
    /// Returns true if the answer indicates it was blocked upstream.
    pub fn is_blocked_upstream(&self, answer: &hickory_proto::op::Message) -> bool {
        is_blocked_upstream(&self.upstream_block_detection, answer)
    }
}

impl fmt::Debug for Resolver {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Resolver")
            .field("config_url", &self.config_url)
            .field("info", &self.info)
            .field("server_address", &self.server_address)
            .field("upstream_block_detection", &self.upstream_block_detection)
            .field("search", &self.search)
            .field("search_only", &self.search_only)
            .field("path", &self.path)
            .field("link_local_unavailable", &self.link_local_unavailable)
            .finish()
    }
}

impl fmt::Display for Resolver {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.info.descriptive_name())
    }
}

/// Scope defines a domain scope and which resolvers can resolve it.
#[derive(Debug, Clone)]
pub struct Scope {
    pub domain: String,
    pub resolvers: Vec<Resolver>,
}
