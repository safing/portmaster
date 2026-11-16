//! Wire protocol of the IVPN daemon, ported from the fields of
//! `github.com/ivpn/desktop-app/daemon/protocol/ivpnclient` that the Go
//! interoperability module actually uses.
//!
//! The daemon speaks newline-delimited JSON on `127.0.0.1:<port>`. Field names
//! are the Go struct field names (Go's `encoding/json` uses them verbatim when
//! no tag is present), so the serde renames below are intentional: they must
//! stay byte-compatible with the daemon and with the Go implementation.

use serde::{Deserialize, Serialize};

/// Message name of `ivpnclient.ConnectionStarting`.
pub const MSG_CONNECTION_STARTING: &str = "ConnectionStarting";
/// Message name of `ivpnclient.ConnectionStopped`.
pub const MSG_CONNECTION_STOPPED: &str = "ConnectionStopped";
/// Message name of `ivpnclient.ConnectedResp`.
pub const MSG_CONNECTED_RESP: &str = "ConnectedResp";
/// Command name of the hello request.
pub const CMD_HELLO: &str = "Hello";
/// Command name of the temporary prioritized DNS request.
pub const CMD_SET_TEMP_PRIORITIZED_DNS: &str = "SetTempPrioritizedDns";

/// Client type reported to the daemon, from `ivpnclient.ClientPortmaster`.
pub const CLIENT_TYPE_PORTMASTER: i32 = 2;

/// Wire value of `ivpnclient.OpenVPN`.
pub const VPN_TYPE_OPENVPN: i32 = 0;
/// Wire value of `ivpnclient.WireGuard`.
pub const VPN_TYPE_WIREGUARD: i32 = 1;

/// IP protocol number of TCP, from `packet.TCP`.
pub const PROTOCOL_TCP: u8 = 6;
/// IP protocol number of UDP, from `packet.UDP`.
pub const PROTOCOL_UDP: u8 = 17;

/// A DNS server entry, mirrors `ivpnclient.DnsServerConfig`.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct DnsServerConfig {
    #[serde(rename = "Address", alias = "address", default)]
    pub address: String,
    #[serde(rename = "Encryption", alias = "encryption", default)]
    pub encryption: i32,
    #[serde(rename = "Template", alias = "template", default)]
    pub template: String,
}

/// DNS settings, mirrors `ivpnclient.DnsSettings`.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct DnsSettings {
    #[serde(rename = "Servers", alias = "servers", default)]
    pub servers: Vec<DnsServerConfig>,
}

impl DnsSettings {
    /// Returns whether no servers are configured, like Go `DnsSettings.IsEmpty`.
    pub fn is_empty(&self) -> bool {
        self.servers.is_empty()
    }
}

/// `ivpnclient.ConnectionStarting`, the notification sent before a VPN
/// connection is established.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConnectionStarting {
    #[serde(rename = "Command", default)]
    pub command: String,
    #[serde(rename = "Idx", default)]
    pub idx: u32,
    #[serde(rename = "Address", alias = "address", default)]
    pub address: String,
    #[serde(rename = "Port", alias = "port", default)]
    pub port: u16,
    #[serde(rename = "Protocol", alias = "protocol", default)]
    pub protocol: u8,
}

/// `ivpnclient.ConnectedResp`, sent when a VPN connection was established.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ConnectedResp {
    #[serde(rename = "Command", default)]
    pub command: String,
    #[serde(rename = "Idx", default)]
    pub idx: u32,
    #[serde(rename = "TimeSecFrom1970", default)]
    pub time_sec_from_1970: i64,
    #[serde(rename = "ClientIP", alias = "client_ip", default)]
    pub client_ip: String,
    #[serde(rename = "ClientIPv6", alias = "client_ip_v6", default)]
    pub client_ipv6: String,
    #[serde(rename = "ServerIP", default)]
    pub server_ip: String,
    #[serde(rename = "ServerPort", default)]
    pub server_port: i32,
    #[serde(rename = "IsTCP", default)]
    pub is_tcp: bool,
    #[serde(rename = "Mtu", default)]
    pub mtu: i32,
    #[serde(rename = "IsPaused", default)]
    pub is_paused: bool,
    #[serde(rename = "PausedTill", default)]
    pub paused_till: String,
    #[serde(rename = "VpnType", alias = "vpn_type", default)]
    pub vpn_type: i32,
}

impl ConnectedResp {
    /// Go `ivpnclient.VpnType.String()`.
    pub fn vpn_type_name(&self) -> &'static str {
        match self.vpn_type {
            VPN_TYPE_OPENVPN => "OpenVPN",
            VPN_TYPE_WIREGUARD => "WireGuard",
            _ => "<Unknown>",
        }
    }
}

/// `ivpnclient.HelloResp`, the reply to the required hello request.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct HelloResp {
    #[serde(rename = "Command", default)]
    pub command: String,
    #[serde(rename = "Idx", default)]
    pub idx: u32,
    #[serde(rename = "Version", alias = "version", default)]
    pub version: String,
    #[serde(rename = "ServiceBinary", alias = "service_binary", default)]
    pub service_binary: String,
    #[serde(rename = "ProcessorArch", default)]
    pub processor_arch: String,
}

/// The minimal envelope every daemon message has, used for command-name and
/// response-index dispatch. Mirrors `ivpnclient.CommandBase`.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct CommandBase {
    #[serde(rename = "Command", default)]
    pub command: String,
    #[serde(rename = "Idx", default)]
    pub idx: u32,
}

/// Parses a `ConnectionStarting` event, like `json.Unmarshal` in the Go handler.
pub fn parse_connection_starting(data: &str) -> Result<ConnectionStarting, serde_json::Error> {
    serde_json::from_str(data)
}

/// Parses a `ConnectedResp` event, like `json.Unmarshal` in the Go handler.
pub fn parse_connected_resp(data: &str) -> Result<ConnectedResp, serde_json::Error> {
    serde_json::from_str(data)
}

/// Parses a `HelloResp` reply.
pub fn parse_hello_resp(data: &str) -> Result<HelloResp, serde_json::Error> {
    serde_json::from_str(data)
}

/// Parses the command name and index of an arbitrary daemon message.
pub fn parse_command_base(data: &str) -> Result<CommandBase, serde_json::Error> {
    serde_json::from_str(data)
}

/// Go `packet.IPProtocol.String()`, used in the connection-starting debug log.
pub fn ip_protocol_name(protocol: u8) -> String {
    match protocol {
        1 => "ICMP".to_string(),
        2 => "IGMP".to_string(),
        PROTOCOL_TCP => "TCP".to_string(),
        PROTOCOL_UDP => "UDP".to_string(),
        58 => "ICMPv6".to_string(),
        136 => "UDPLite".to_string(),
        255 => "RAW".to_string(),
        other => format!("<unknown protocol, {other}>"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_connection_starting_with_go_field_names() {
        let raw = r#"{"Command":"ConnectionStarting","Idx":3,"Address":"203.0.113.7","Port":1194,"Protocol":17}"#;
        let msg = parse_connection_starting(raw).expect("parse");
        assert_eq!(msg.command, "ConnectionStarting");
        assert_eq!(msg.idx, 3);
        assert_eq!(msg.address, "203.0.113.7");
        assert_eq!(msg.port, 1194);
        assert_eq!(msg.protocol, PROTOCOL_UDP);
    }

    #[test]
    fn parses_connection_starting_with_missing_fields() {
        let msg = parse_connection_starting("{}").expect("parse");
        assert_eq!(msg, ConnectionStarting::default());
    }

    #[test]
    fn rejects_malformed_connection_starting() {
        assert!(parse_connection_starting(r#"{"Port":"not-a-number"}"#).is_err());
    }

    #[test]
    fn parses_connected_resp() {
        let raw = r#"{"Command":"ConnectedResp","VpnType":1,"ClientIP":"10.50.0.2","ClientIPv6":"","TimeSecFrom1970":1700000000}"#;
        let msg = parse_connected_resp(raw).expect("parse");
        assert_eq!(msg.vpn_type, VPN_TYPE_WIREGUARD);
        assert_eq!(msg.vpn_type_name(), "WireGuard");
        assert_eq!(msg.client_ip, "10.50.0.2");
        assert_eq!(msg.time_sec_from_1970, 1700000000);
    }

    #[test]
    fn connected_resp_unknown_vpn_type() {
        let msg = parse_connected_resp(r#"{"VpnType":42}"#).expect("parse");
        assert_eq!(msg.vpn_type_name(), "<Unknown>");
    }

    #[test]
    fn parses_hello_resp_and_notice_missing_service_binary() {
        let msg = parse_hello_resp(r#"{"Version":"3.15.2"}"#).expect("parse");
        assert_eq!(msg.version, "3.15.2");
        assert!(msg.service_binary.is_empty());
    }

    #[test]
    fn ignores_unknown_fields_like_go_json() {
        let msg = parse_connection_starting(
            r#"{"Address":"1.2.3.4","Port":443,"Protocol":6,"Extra":[1,2,3]}"#,
        )
        .expect("parse");
        assert_eq!(msg.address, "1.2.3.4");
        assert_eq!(msg.protocol, PROTOCOL_TCP);
    }

    #[test]
    fn dns_settings_serialize_with_go_field_names() {
        let dns = DnsSettings {
            servers: vec![DnsServerConfig {
                address: "127.0.0.1".into(),
                ..DnsServerConfig::default()
            }],
        };
        let value = serde_json::to_value(&dns).expect("serialize");
        assert_eq!(value["Servers"][0]["Address"], "127.0.0.1");
        assert!(!dns.is_empty());
        assert!(DnsSettings::default().is_empty());
    }

    #[test]
    fn protocol_names_match_go() {
        assert_eq!(ip_protocol_name(6), "TCP");
        assert_eq!(ip_protocol_name(17), "UDP");
        assert_eq!(ip_protocol_name(99), "<unknown protocol, 99>");
    }
}
