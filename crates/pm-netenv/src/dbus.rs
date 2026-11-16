//! Minimal D-Bus access for NetworkManager properties, port of the parts of Go
//! `service/netenv/dbus_linux.go` that are relevant to the network environment.
//!
//! # Deviation from Go
//!
//! Go uses `github.com/godbus/dbus/v5` to talk to the system bus directly.
//! Adding a native D-Bus client would pull a new dependency into this crate, so
//! the system implementation shells out to `busctl` (part of systemd) and parses
//! its textual output. The interesting logic is behind [`DbusClient`], which is
//! injected into [`nameservers_from_dbus`] and [`connectivity_state_from_dbus`]
//! so it can be tested without a system bus. If `busctl` is missing or the call
//! fails, the callers fall back to `/etc/resolv.conf`, just like Go logs the
//! D-Bus error and continues with the file-based nameservers.

use std::process::Command;

use crate::environment::Nameserver;
use crate::OnlineStatus;

/// NetworkManager bus name.
pub const NM_SERVICE: &str = "org.freedesktop.NetworkManager";
/// NetworkManager root object path.
pub const NM_OBJECT: &str = "/org/freedesktop/NetworkManager";
/// NetworkManager interface of the root object.
pub const NM_INTERFACE: &str = "org.freedesktop.NetworkManager";
/// Active connection interface.
pub const NM_ACTIVE_CONNECTION_INTERFACE: &str = "org.freedesktop.NetworkManager.Connection.Active";
/// IPv4 configuration interface.
pub const NM_IP4_CONFIG_INTERFACE: &str = "org.freedesktop.NetworkManager.IP4Config";
/// IPv6 configuration interface.
pub const NM_IP6_CONFIG_INTERFACE: &str = "org.freedesktop.NetworkManager.IP6Config";

/// A parsed D-Bus property value.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DbusValue {
    /// Unsigned 32 bit integer.
    U32(u32),
    /// D-Bus object path.
    ObjectPath(String),
    /// Array of strings.
    StringArray(Vec<String>),
    /// Array of unsigned 32 bit integers.
    U32Array(Vec<u32>),
    /// Array of byte arrays (NetworkManager's IPv6 nameserver format).
    ByteArrayArray(Vec<Vec<u8>>),
    /// A value this parser does not need.
    Other(String),
}

/// Access to D-Bus properties, injected so the Go logic is testable.
pub trait DbusClient: Send + Sync {
    /// Reads one property of an object.
    fn get_property(
        &self,
        service: &str,
        object: &str,
        interface: &str,
        property: &str,
    ) -> Result<DbusValue, String>;
}

/// Reads properties with `busctl` from the system bus.
#[derive(Debug, Default, Clone, Copy)]
pub struct SystemDbus;

impl DbusClient for SystemDbus {
    fn get_property(
        &self,
        service: &str,
        object: &str,
        interface: &str,
        property: &str,
    ) -> Result<DbusValue, String> {
        let output = Command::new("busctl")
            .args([
                "--system",
                "--no-pager",
                "get-property",
                service,
                object,
                interface,
                property,
            ])
            .output()
            .map_err(|error| format!("failed to run busctl: {error}"))?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
            return Err(format!(
                "busctl get-property {interface}.{property} failed: {stderr}"
            ));
        }
        Ok(parse_busctl_value(&String::from_utf8_lossy(&output.stdout)))
    }
}

/// Parses one line of `busctl get-property` output.
///
/// Known scalar and array signatures are decoded; anything else is kept as
/// [`DbusValue::Other`] so it can still be inspected by callers such as the
/// connectivity-state parser.
pub fn parse_busctl_value(output: &str) -> DbusValue {
    let line = output.trim().lines().next().unwrap_or_default().trim();
    let parts: Vec<&str> = line.split_whitespace().collect();
    let Some(signature) = parts.first().copied() else {
        return DbusValue::Other(String::new());
    };

    // Scalars are printed as `uint32 4` or `os "/path"` - the second token is
    // the value, not an array count.
    if parts.len() == 2 && matches!(signature, "u" | "uint32") {
        return parts[1]
            .parse::<u32>()
            .map_or(DbusValue::Other(line.to_string()), DbusValue::U32);
    }
    if parts.len() == 2 && matches!(signature, "o" | "os") {
        return DbusValue::ObjectPath(unquote(parts[1]));
    }

    let mut tokens = parts.into_iter();
    let _signature = tokens.next();
    let Some(count) = tokens.next().and_then(|value| value.parse::<usize>().ok()) else {
        return DbusValue::Other(line.to_string());
    };

    match signature {
        "u" | "uint32" if count == 1 => tokens
            .next()
            .and_then(|value| value.parse::<u32>().ok())
            .map_or(DbusValue::Other(line.to_string()), DbusValue::U32),
        "o" | "os" if count == 1 => tokens
            .next()
            .map(|value| DbusValue::ObjectPath(unquote(value)))
            .unwrap_or_else(|| DbusValue::Other(line.to_string())),
        "as" => DbusValue::StringArray(tokens.take(count).map(unquote).collect()),
        "au" => DbusValue::U32Array(
            tokens
                .take(count)
                .filter_map(|value| value.parse::<u32>().ok())
                .collect(),
        ),
        "aay" => {
            let mut arrays = Vec::new();
            for _ in 0..count {
                let Some(len) = tokens.next().and_then(|value| value.parse::<usize>().ok()) else {
                    break;
                };
                let bytes: Vec<u8> = tokens
                    .by_ref()
                    .take(len)
                    .filter_map(|value| value.parse::<u8>().ok())
                    .collect();
                arrays.push(bytes);
            }
            DbusValue::ByteArrayArray(arrays)
        }
        _ => DbusValue::Other(line.to_string()),
    }
}

fn unquote(value: &str) -> String {
    value
        .trim_matches(|char| char == '"' || char == '\'')
        .to_string()
}

/// Connectivity state as reported by NetworkManager (`NMConnectivityState`).
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum ConnectivityState {
    #[default]
    Unknown,
    None,
    Portal,
    Limited,
    Full,
}

impl ConnectivityState {
    /// Maps a raw `NMConnectivityState` value, like the Go switch statement.
    pub fn from_network_manager(value: u32) -> ConnectivityState {
        match value {
            1 => ConnectivityState::None,
            2 => ConnectivityState::Portal,
            3 => ConnectivityState::Limited,
            4 => ConnectivityState::Full,
            _ => ConnectivityState::Unknown,
        }
    }

    /// Maps the state to a Portmaster online status.
    pub fn online_status(self) -> OnlineStatus {
        match self {
            ConnectivityState::Unknown => OnlineStatus::Unknown,
            ConnectivityState::None => OnlineStatus::Offline,
            ConnectivityState::Portal => OnlineStatus::Portal,
            ConnectivityState::Limited => OnlineStatus::Limited,
            ConnectivityState::Full => OnlineStatus::Online,
        }
    }
}

/// Reads the NetworkManager connectivity state, like Go
/// `getConnectivityStateFromDbus`.
pub fn connectivity_state_from_dbus(client: &dyn DbusClient) -> Result<ConnectivityState, String> {
    let value = client.get_property(NM_SERVICE, NM_OBJECT, NM_INTERFACE, "Connectivity")?;
    let raw = match value {
        DbusValue::U32(raw) => raw,
        DbusValue::Other(line) => parse_connectivity_u32(&line)?,
        other => return Err(format!("unexpected connectivity property: {other:?}")),
    };
    Ok(ConnectivityState::from_network_manager(raw))
}

/// Extracts a `uint32` from tolerant D-Bus scalar output, e.g.
/// `uint32 4`, `<uint32 4>` or `(uint32 4,)`.
pub fn parse_connectivity_u32(value: &str) -> Result<u32, String> {
    for token in value.split_whitespace() {
        let cleaned = token
            .trim_matches(|char: char| !char.is_ascii_alphanumeric() && char != '-' && char != '+');
        if let Ok(raw) = cleaned.parse::<u32>() {
            return Ok(raw);
        }
    }
    Err(format!("could not parse connectivity state from {value:?}"))
}

/// Returns the nameservers configured on active NetworkManager connections,
/// like Go `getNameserversFromDbus`.
///
/// The primary connection is processed first, followed by the remaining active
/// connections. IPv4 and IPv6 nameservers are collected for each connection in
/// that order. Individual connection failures are logged and skipped, matching
/// Go's behavior.
pub fn nameservers_from_dbus(client: &dyn DbusClient) -> Result<Vec<Nameserver>, String> {
    let primary = get_object_path(client, NM_OBJECT, NM_INTERFACE, "PrimaryConnection")?;
    let active = get_object_paths(client, NM_OBJECT, NM_INTERFACE, "ActiveConnections")?;

    let mut sorted = vec![primary];
    for path in active {
        if !sorted.contains(&path) {
            sorted.push(path);
        }
    }

    let mut nameservers = Vec::new();
    for connection in sorted {
        for version in [4, 6] {
            match interface_nameservers(client, &connection, version) {
                Ok(found) => nameservers.extend(found),
                Err(error) => tracing::warn!("netenv: dbus nameserver lookup failed: {error}"),
            }
        }
    }
    Ok(nameservers)
}

fn interface_nameservers(
    client: &dyn DbusClient,
    connection: &str,
    version: u8,
) -> Result<Vec<Nameserver>, String> {
    let (active_interface, config_interface) = match version {
        4 => (NM_ACTIVE_CONNECTION_INTERFACE, NM_IP4_CONFIG_INTERFACE),
        6 => (NM_ACTIVE_CONNECTION_INTERFACE, NM_IP6_CONFIG_INTERFACE),
        other => return Err(format!("unsupported IP version: {other}")),
    };
    let config_key = format!("Ip{version}Config");

    let config = get_object_path(client, connection, active_interface, &config_key)?;
    if config.is_empty() || config == "/" {
        return Ok(Vec::new());
    }

    let addresses = client.get_property(NM_SERVICE, &config, config_interface, "Nameservers")?;
    let mut ips = Vec::new();
    match (version, addresses) {
        (4, DbusValue::U32Array(values)) => {
            for value in values {
                let a = (value / 16_777_216) as u8;
                let b = ((value % 16_777_216) / 65_536) as u8;
                let c = ((value % 65_536) / 256) as u8;
                let d = (value % 256) as u8;
                ips.push(std::net::IpAddr::V4(std::net::Ipv4Addr::new(d, c, b, a)));
            }
        }
        (6, DbusValue::ByteArrayArray(values)) => {
            for value in values {
                let bytes: [u8; 16] = value
                    .try_into()
                    .map_err(|_| "query returned IPv6 address with invalid length".to_string())?;
                ips.push(std::net::IpAddr::V6(std::net::Ipv6Addr::from(bytes)));
            }
        }
        (version, other) => {
            return Err(format!(
                "unexpected {version} nameserver property: {other:?}"
            ))
        }
    }

    let domains = get_string_array(client, &config, config_interface, "Domains")?;
    let searches = get_string_array(client, &config, config_interface, "Searches")?;
    let search = [domains, searches].concat();

    Ok(ips
        .into_iter()
        .map(|ip| Nameserver {
            ip: crate::interfaces::normalize_ip(ip),
            search: search.clone(),
        })
        .collect())
}

fn get_object_path(
    client: &dyn DbusClient,
    object: &str,
    interface: &str,
    property: &str,
) -> Result<String, String> {
    match client.get_property(NM_SERVICE, object, interface, property)? {
        DbusValue::ObjectPath(path) => Ok(path),
        other => Err(format!(
            "could not assert type of {interface}.{property}: {other:?}"
        )),
    }
}

fn get_object_paths(
    client: &dyn DbusClient,
    object: &str,
    interface: &str,
    property: &str,
) -> Result<Vec<String>, String> {
    match client
        .get_property(NM_SERVICE, object, interface, property)?
        .to_object_paths()
    {
        Some(paths) => Ok(paths),
        None => Err(format!("could not assert type of {interface}.{property}")),
    }
}

fn get_string_array(
    client: &dyn DbusClient,
    object: &str,
    interface: &str,
    property: &str,
) -> Result<Vec<String>, String> {
    match client.get_property(NM_SERVICE, object, interface, property)? {
        DbusValue::StringArray(values) => Ok(values),
        other => Err(format!(
            "could not assert type of {interface}.{property}: {other:?}"
        )),
    }
}

impl DbusValue {
    /// Returns the value as object paths, including `ao` arrays parsed from a
    /// line that carries more tokens than expected.
    pub fn to_object_paths(&self) -> Option<Vec<String>> {
        match self {
            DbusValue::ObjectPath(path) => Some(vec![path.clone()]),
            DbusValue::Other(line) => {
                let mut tokens = line.split_whitespace();
                let signature = tokens.next()?;
                if signature != "ao" && signature != "as" {
                    return None;
                }
                let _count = tokens.next()?.parse::<usize>().ok()?;
                Some(tokens.map(unquote).collect())
            }
            _ => None,
        }
    }
}
