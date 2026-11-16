//! Port of Go `config.go` and the address parsing of `module.go`.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};

use pm_config::{
    Config, ConfigOption, CATEGORY_ANNOTATION, DISPLAY_ORDER_ANNOTATION, EXPERTISE_LEVEL_DEVELOPER,
    OPT_TYPE_STRING, RELEASE_LEVEL_STABLE,
};
use serde_json::json;

/// The config key of the listen address.
pub const CFG_DEFAULT_NAMESERVER_ADDRESS_KEY: &str = "dns/listenAddress";

/// The address `localhost` stands for on the IPv4 side.
pub const LOCALHOST_V4: Ipv4Addr = Ipv4Addr::new(127, 0, 0, 17);

/// Returns the default listen address, the default of the
/// `--nameserver-address` flag.
///
/// On Windows packets are redirected to the same interface, so the default
/// listens on all addresses.
pub fn default_nameserver_address() -> &'static str {
    if cfg!(windows) {
        "0.0.0.0:53"
    } else {
        "localhost:53"
    }
}

/// Registers the `dns/listenAddress` option, like Go `registerConfig`.
///
/// `flag_address` is the value of the `--nameserver-address` flag (or
/// [`default_nameserver_address`] when it is not given). It becomes the
/// default of the option; a configured value is stronger.
pub fn register_options(config: &Config, flag_address: &str) -> Result<(), pm_config::Error> {
    config.register(
        ConfigOption::new(
            "Internal DNS Server Listen Address",
            CFG_DEFAULT_NAMESERVER_ADDRESS_KEY,
            "Defines the IP address and port on which the internal DNS Server listens.",
            OPT_TYPE_STRING,
            json!(flag_address),
        )
        .with_expertise_level(EXPERTISE_LEVEL_DEVELOPER)
        .with_release_level(RELEASE_LEVEL_STABLE)
        .with_validation_regex(
            "^(localhost|[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}.[0-9]{1,3}|\\[[:0-9A-Fa-f]+\\]):[0-9]{1,5}$",
        )
        .with_requires_restart(true)
        .with_annotation(DISPLAY_ORDER_ANNOTATION, json!(514))
        .with_annotation(CATEGORY_ANNOTATION, json!("Development")),
    )
}

/// The addresses the nameserver listens on.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ListenAddresses {
    pub ip1: IpAddr,
    /// The second listener, only used for `localhost` with an IPv6 stack.
    pub ip2: Option<IpAddr>,
    pub port: u16,
}

/// A listen address that cannot be used.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum ListenAddressError {
    #[error("failed to parse address {address}: {reason}")]
    Address { address: String, reason: String },
    #[error("failed to parse IP {ip} from {address}")]
    Ip { ip: String, address: String },
    #[error("failed to parse port {port} from {address}: {reason}")]
    Port {
        port: String,
        address: String,
        reason: String,
    },
}

/// Like Go `net.SplitHostPort`.
fn split_host_port(address: &str) -> Result<(&str, &str), String> {
    let Some(colon) = address.rfind(':') else {
        return Err(format!("address {address}: missing port in address"));
    };
    let (host, port) = (&address[..colon], &address[colon + 1..]);
    if let Some(rest) = host.strip_prefix('[') {
        let Some(inner) = rest.strip_suffix(']') else {
            return Err(format!("address {address}: missing ']' in address"));
        };
        if inner.contains('[') || inner.contains(']') {
            return Err(format!("address {address}: unexpected '[' in address"));
        }
        return Ok((inner, port));
    }
    if host.contains(':') {
        return Err(format!("address {address}: too many colons in address"));
    }
    if host.contains('[') || host.contains(']') {
        return Err(format!("address {address}: unexpected bracket in address"));
    }
    Ok((host, port))
}

/// Parses the listen address, like Go `getListenAddresses`.
///
/// `localhost` means two listeners: `127.0.0.17` and, if the machine has an
/// IPv6 stack, `::1`.
pub fn parse_listen_addresses(
    listen_address: &str,
    ipv6_enabled: bool,
) -> Result<ListenAddresses, ListenAddressError> {
    let (ip_string, port_string) =
        split_host_port(listen_address).map_err(|reason| ListenAddressError::Address {
            address: listen_address.to_string(),
            reason,
        })?;

    let (ip1, ip2) = if ip_string == "localhost" {
        let ip2 = if ipv6_enabled {
            Some(IpAddr::V6(Ipv6Addr::LOCALHOST))
        } else {
            tracing::warn!(
                "nameserver: no IPv6 stack detected, disabling IPv6 nameserver listener"
            );
            None
        };
        (IpAddr::V4(LOCALHOST_V4), ip2)
    } else {
        let ip = ip_string
            .parse::<IpAddr>()
            .map_err(|_| ListenAddressError::Ip {
                ip: ip_string.to_string(),
                address: listen_address.to_string(),
            })?;
        (ip, None)
    };

    let port = port_string
        .parse::<u16>()
        .map_err(|e| ListenAddressError::Port {
            port: port_string.to_string(),
            address: listen_address.to_string(),
            reason: e.to_string(),
        })?;

    Ok(ListenAddresses { ip1, ip2, port })
}
