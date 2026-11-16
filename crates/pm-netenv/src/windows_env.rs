//! Windows nameserver and gateway lookups, port of Go
//! `service/netenv/environment_windows.go`.
//!
//! Go asks PowerShell for the default interface of the `0.0.0.0/0` route and
//! parses the `Format-List` output, then reads the DNS suffix configuration
//! with `Get-DnsClient`. The same commands and parsing are used here. No unsafe
//! FFI is needed: the data comes from the PowerShell process.
//!
//! The OS-facing parts are gated to `#[cfg(windows)]`. The parsers are compiled
//! on every platform (with `#[cfg(any(windows, test))]`) so their unit tests
//! run in the normal host test suite. Gateways carry the interface index in
//! place of a name, since that is the only identifier the Go data provides.

#[cfg(any(windows, test))]
use std::net::IpAddr;

#[cfg(any(windows, test))]
use crate::environment::{GatewayInfo, Nameserver};

/// DNS suffix configuration of an interface (Go `dnsServerConfig`).
#[cfg(any(windows, test))]
#[derive(Debug, Default, PartialEq, Eq)]
struct DnsServerConfig {
    suffix: Option<String>,
    suffix_search_list: Vec<String>,
}

/// Default interface data gathered from Windows (Go `DefaultNetInterface`).
#[cfg(any(windows, test))]
#[derive(Debug, Default, PartialEq, Eq)]
struct DefaultNetInterface {
    interface_index: String,
    ipv4_default_gateway: Option<IpAddr>,
    ipv6_default_gateway: Option<IpAddr>,
    dns_server: Vec<IpAddr>,
    dns_config: Option<DnsServerConfig>,
}

/// Parses the `Format-List` output of `Get-NetIPConfiguration`, like Go's
/// `GetDefaultInterface` scanner. Continuation lines repeat the previous key.
#[cfg(any(windows, test))]
fn parse_interface_list(output: &str) -> DefaultNetInterface {
    let mut default_if = DefaultNetInterface::default();
    let mut previous_key = String::new();

    for line in output.lines() {
        let (key, value) = match line.split_once(" : ") {
            Some((key, value)) => {
                let key = key.trim();
                previous_key = key.to_string();
                (key.to_string(), value.trim().to_string())
            }
            None => (previous_key.clone(), line.trim().to_string()),
        };
        if value.is_empty() {
            continue;
        }

        match key.as_str() {
            "InterfaceIndex" => default_if.interface_index = value,
            "IPv4DefaultGateway" => default_if.ipv4_default_gateway = value.parse().ok(),
            "IPv6DefaultGateway" => default_if.ipv6_default_gateway = value.parse().ok(),
            "DNSServer" => {
                if let Ok(ip) = value.parse::<IpAddr>() {
                    default_if.dns_server.push(ip);
                }
            }
            _ => {}
        }
    }

    default_if
}

/// Parses the JSON of `Get-DnsClient | ConvertTo-Json`, tolerating both a
/// single object and a one-element array.
#[cfg(any(windows, test))]
fn parse_dns_config(json: &str) -> DnsServerConfig {
    let Ok(value) = serde_json::from_str::<serde_json::Value>(json) else {
        return DnsServerConfig::default();
    };
    let value = if value.is_array() {
        value.as_array().and_then(|array| array.first())
    } else {
        Some(&value)
    };
    let Some(object) = value.and_then(serde_json::Value::as_object) else {
        return DnsServerConfig::default();
    };

    let suffix = object
        .get("Suffix")
        .and_then(serde_json::Value::as_str)
        .filter(|suffix| !suffix.is_empty())
        .map(str::to_string);
    let suffix_search_list = object
        .get("SuffixSearchList")
        .and_then(serde_json::Value::as_array)
        .map(|domains| {
            domains
                .iter()
                .filter_map(serde_json::Value::as_str)
                .map(str::to_string)
                .collect()
        })
        .unwrap_or_default();

    DnsServerConfig {
        suffix,
        suffix_search_list,
    }
}

/// Nameservers of the default interface, like Go `Nameservers`.
#[cfg(any(windows, test))]
fn nameservers_from(default_if: &DefaultNetInterface) -> Vec<Nameserver> {
    let mut search = Vec::new();
    if let Some(config) = &default_if.dns_config {
        search.extend(config.suffix.iter().cloned());
        search.extend(config.suffix_search_list.iter().cloned());
    }
    default_if
        .dns_server
        .iter()
        .map(|&ip| Nameserver {
            ip,
            search: search.clone(),
        })
        .collect()
}

/// Default gateways of the default interface, like Go `Gateways`.
#[cfg(any(windows, test))]
fn gateways_from(default_if: &DefaultNetInterface) -> Vec<GatewayInfo> {
    let mut gateways = Vec::new();
    if let Some(ip) = default_if.ipv4_default_gateway {
        gateways.push(GatewayInfo {
            ip,
            interface: default_if.interface_index.clone(),
            mask: "0.0.0.0/0".to_string(),
        });
    }
    if let Some(ip) = default_if.ipv6_default_gateway {
        gateways.push(GatewayInfo {
            ip,
            interface: default_if.interface_index.clone(),
            mask: "::/0".to_string(),
        });
    }
    gateways
}

/// Runs a PowerShell script with the same flags as Go
/// `osdetail.RunPowershellCmd`.
#[cfg(windows)]
fn run_powershell(script: &str) -> Result<String, String> {
    let script = format!(
        "[System.Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n{script}"
    );
    let output = std::process::Command::new("powershell.exe")
        .args([
            "-ExecutionPolicy",
            "Bypass",
            "-NoProfile",
            "-NonInteractive",
            "-Command",
            &script,
        ])
        .output()
        .map_err(|error| format!("failed to run powershell: {error}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        return Err(format!("powershell failed: {stderr}"));
    }
    Ok(String::from_utf8_lossy(&output.stdout).into_owned())
}

/// Queries and parses the current default interface.
#[cfg(windows)]
fn default_interface() -> Result<DefaultNetInterface, String> {
    const INTERFACE_SCRIPT: &str = "Get-NetRoute -DestinationPrefix '0.0.0.0/0' \
         | Select-Object -First 1 | Get-NetIPConfiguration | Format-List";

    let output = run_powershell(INTERFACE_SCRIPT)?;
    let mut default_if = parse_interface_list(&output);
    if default_if.interface_index.is_empty() {
        tracing::warn!("netenv: could not get dns server config: default interface index is missing");
        return Ok(default_if);
    }

    let script = format!(
        "Get-DnsClient -InterfaceIndex {} | ConvertTo-Json -Depth 1",
        default_if.interface_index
    );
    match run_powershell(&script) {
        Ok(json) => default_if.dns_config = Some(parse_dns_config(&json)),
        Err(error) => tracing::warn!("netenv: failed to get dns server config: {error}"),
    }
    Ok(default_if)
}

/// Returns the currently configured nameservers.
#[cfg(windows)]
pub(crate) fn nameservers() -> Result<Vec<Nameserver>, String> {
    Ok(nameservers_from(&default_interface()?))
}

/// Returns the active default gateways.
#[cfg(windows)]
pub(crate) fn gateways() -> Result<Vec<GatewayInfo>, String> {
    Ok(gateways_from(&default_interface()?))
}

#[cfg(test)]
mod tests {
    use super::*;

    const FORMAT_LIST: &str = "\
\r
InterfaceIndex     : 12\r
IPv4Address        : {192.168.1.23; 192.168.1.24}\r
IPv4DefaultGateway : 192.168.1.1\r
IPv6DefaultGateway : fe80::1\r
DNSServer          : 192.168.1.1\r
                     1.1.1.1\r
                     2606:4700:4700::1111\r
";

    #[test]
    fn parses_default_interface_format_list() {
        let parsed = parse_interface_list(FORMAT_LIST);
        assert_eq!(parsed.interface_index, "12");
        assert_eq!(
            parsed.ipv4_default_gateway,
            Some("192.168.1.1".parse().unwrap())
        );
        assert_eq!(parsed.ipv6_default_gateway, Some("fe80::1".parse().unwrap()));
        assert_eq!(parsed.dns_server.len(), 3, "continuation lines are values");
        assert_eq!(parsed.dns_server[1], "1.1.1.1".parse::<IpAddr>().unwrap());
    }

    #[test]
    fn parses_dns_client_json() {
        let config = parse_dns_config(
            r#"{"InterfaceIndex":12,"Suffix":"example.test","SuffixSearchList":["a.test","b.test"]}"#,
        );
        assert_eq!(config.suffix.as_deref(), Some("example.test"));
        assert_eq!(config.suffix_search_list, ["a.test", "b.test"]);
        assert_eq!(parse_dns_config("not json"), DnsServerConfig::default());
        assert_eq!(parse_dns_config("[]"), DnsServerConfig::default());
    }

    #[test]
    fn builds_nameservers_and_gateways() {
        let mut default_if = parse_interface_list(FORMAT_LIST);
        default_if.dns_config = Some(parse_dns_config(
            r#"{"Suffix":"example.test","SuffixSearchList":["a.test"]}"#,
        ));

        let nameservers = nameservers_from(&default_if);
        assert_eq!(nameservers.len(), 3);
        assert_eq!(
            nameservers[0].search,
            ["example.test".to_string(), "a.test".to_string()]
        );

        let gateways = gateways_from(&default_if);
        assert_eq!(gateways.len(), 2);
        assert_eq!(gateways[0].interface, "12");
        assert_eq!(gateways[0].mask, "0.0.0.0/0");
        assert_eq!(gateways[1].mask, "::/0");
    }
}
