//! Generation of Xray JSON configuration for a selected node.
//!
//! The shape mirrors the canonical configurations shipped by xray-rs
//! (`config example socks-direct` / `vless-tls` and the vmess/trojan/
//! shadowsocks fixtures): a localhost SOCKS or HTTP inbound named
//! `socks-in`, a single `proxy` outbound and an empty `routing` section.

use std::path::{Path, PathBuf};

use serde_json::{json, Map, Value};

use crate::model::{Protocol, XrayNode};
use crate::parser::{normalize_network, parse_link, ParsedProfile};
use crate::{Error, Result};

/// Default local SOCKS port.
pub const DEFAULT_INBOUND_PORT: u16 = 1080;
/// Default directory name for generated configs.
pub const CONFIG_DIR: &str = "xray";

/// Inbound protocol exposed to the rest of the system.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum InboundProtocol {
    Socks,
    Http,
}

impl InboundProtocol {
    pub fn as_str(&self) -> &'static str {
        match self {
            InboundProtocol::Socks => "socks",
            InboundProtocol::Http => "http",
        }
    }
}

fn stream_settings(profile: &ParsedProfile) -> Value {
    let security = if profile.protocol == Protocol::Trojan && profile.security == "none" {
        // Trojan requires authenticated TLS; share links without an explicit
        // security parameter still mean TLS.
        "tls".to_string()
    } else {
        profile.security.clone()
    };
    let mut stream = Map::new();
    stream.insert("network".into(), json!(profile.network));
    stream.insert("security".into(), json!(security));

    if security == "tls" || security == "reality" {
        let mut tls = Map::new();
        let server = if profile.sni.is_empty() {
            profile.address.clone()
        } else {
            profile.sni.clone()
        };
        tls.insert("serverName".into(), json!(server));
        if !profile.fingerprint.is_empty() {
            tls.insert("fingerprint".into(), json!(profile.fingerprint));
        }
        if !profile.alpn.is_empty() {
            tls.insert("alpn".into(), json!(profile.alpn));
        }
        stream.insert("tlsSettings".into(), Value::Object(tls));
    }

    match normalize_network(&profile.network).as_str() {
        "ws" => {
            let mut ws = Map::new();
            if !profile.path.is_empty() {
                ws.insert("path".into(), json!(profile.path));
            }
            if !profile.host.is_empty() {
                ws.insert("host".into(), json!(profile.host));
            }
            stream.insert("wsSettings".into(), Value::Object(ws));
        }
        "grpc" => {
            let mut grpc = Map::new();
            if !profile.path.is_empty() {
                grpc.insert("serviceName".into(), json!(profile.path));
            }
            stream.insert("grpcSettings".into(), Value::Object(grpc));
        }
        "xhttp" => {
            let mut xhttp = Map::new();
            if !profile.path.is_empty() {
                xhttp.insert("path".into(), json!(profile.path));
            }
            if !profile.host.is_empty() {
                xhttp.insert("host".into(), json!(profile.host));
            }
            stream.insert("xhttpSettings".into(), Value::Object(xhttp));
        }
        _ => {}
    }
    Value::Object(stream)
}

fn outbound_settings(profile: &ParsedProfile) -> Result<Value> {
    let settings = match profile.protocol {
        Protocol::Vless => {
            let mut user = Map::new();
            user.insert("id".into(), json!(profile.credential));
            user.insert("encryption".into(), json!("none"));
            let mut server = Map::new();
            server.insert("address".into(), json!(profile.address));
            server.insert("port".into(), json!(profile.port));
            server.insert("users".into(), json!(vec![Value::Object(user)]));
            json!({ "vnext": [Value::Object(server)] })
        }
        Protocol::Vmess => {
            json!({
                "address": profile.address,
                "port": profile.port,
                "id": profile.credential,
                "security": "auto",
            })
        }
        Protocol::Trojan => {
            json!({
                "address": profile.address,
                "port": profile.port,
                "password": profile.password,
            })
        }
        Protocol::Shadowsocks => {
            if profile.method.is_empty() || profile.password.is_empty() {
                return Err(Error::Config("Shadowsocks needs method and password".into()));
            }
            json!({
                "address": profile.address,
                "port": profile.port,
                "method": profile.method,
                "password": profile.password,
            })
        }
        Protocol::Socks | Protocol::Http => {
            let mut settings = Map::new();
            settings.insert("address".into(), json!(profile.address));
            settings.insert("port".into(), json!(profile.port));
            if !profile.credential.is_empty() {
                settings.insert("user".into(), json!(profile.credential));
                settings.insert("pass".into(), json!(profile.password));
            }
            Value::Object(settings)
        }
        Protocol::Unknown => {
            return Err(Error::Config("cannot build config for unknown protocol".into()));
        }
    };
    Ok(settings)
}

/// Builds the full Xray JSON configuration for a node without a stats API.
pub fn generate_config(
    node: &XrayNode,
    inbound_port: u16,
    inbound: InboundProtocol,
) -> Result<Value> {
    generate_config_with_stats(node, inbound_port, inbound, 0)
}

/// Builds the full Xray JSON configuration for a node.
///
/// When `stats_port` is non-zero an `api` inbound with `StatsService` is
/// added, together with the `stats` section and outbound traffic policies,
/// so the gRPC StatsService client has a server to talk to.
pub fn generate_config_with_stats(
    node: &XrayNode,
    inbound_port: u16,
    inbound: InboundProtocol,
    stats_port: u16,
) -> Result<Value> {
    if inbound_port == 0 {
        return Err(Error::Config("inbound port must not be zero".into()));
    }
    let profile = parse_link(&node.raw_config)?;
    if profile.address.is_empty() || profile.port == 0 {
        return Err(Error::Config("node has no usable server address".into()));
    }
    let mut outbound = Map::new();
    outbound.insert("tag".into(), json!("proxy"));
    outbound.insert("protocol".into(), json!(profile.protocol.as_str()));
    outbound.insert("settings".into(), outbound_settings(&profile)?);
    outbound.insert("streamSettings".into(), stream_settings(&profile));

    let inbound_settings = match inbound {
        InboundProtocol::Socks => json!({ "auth": "noauth", "udp": true }),
        InboundProtocol::Http => json!({ "auth": "noauth" }),
    };
    let mut config = json!({
        "inbounds": [{
            "tag": "socks-in",
            "protocol": inbound.as_str(),
            "listen": "127.0.0.1",
            "port": inbound_port,
            "settings": inbound_settings,
        }],
        "outbounds": [Value::Object(outbound)],
        "routing": {
            "domainStrategy": "AsIs",
            "rules": [],
        },
    });
    if stats_port != 0 {
        config["api"] = json!({
            "tag": "api",
            "services": ["StatsService"],
        });
        config["inbounds"].as_array_mut().expect("inbounds array").push(json!({
            "tag": "api",
            "listen": "127.0.0.1",
            "port": stats_port,
            "protocol": "tunnel",
            "settings": { "address": "127.0.0.1" },
        }));
        config["stats"] = json!({});
        config["policy"] = json!({
            "system": {
                "statsOutboundUplink": true,
                "statsOutboundDownlink": true,
            },
        });
    }
    Ok(config)
}

/// Writes a generated config into `data_dir/xray/xray-<node id>.json` and
/// returns its path.
pub fn write_config(
    data_dir: &Path,
    node: &XrayNode,
    inbound_port: u16,
    inbound: InboundProtocol,
) -> Result<PathBuf> {
    write_config_with_stats(data_dir, node, inbound_port, inbound, 0)
}

/// Like [`write_config`], but includes the gRPC stats API inbound.
pub fn write_config_with_stats(
    data_dir: &Path,
    node: &XrayNode,
    inbound_port: u16,
    inbound: InboundProtocol,
    stats_port: u16,
) -> Result<PathBuf> {
    let directory = data_dir.join(CONFIG_DIR);
    std::fs::create_dir_all(&directory).map_err(|error| Error::Config(error.to_string()))?;
    let path = directory.join(format!("xray-{}.json", node.id));
    let config = generate_config_with_stats(node, inbound_port, inbound, stats_port)?;
    let text = serde_json::to_string_pretty(&config).map_err(|error| Error::Config(error.to_string()))?;
    std::fs::write(&path, text).map_err(|error| Error::Config(error.to_string()))?;
    Ok(path)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::Protocol;

    fn node(raw: &str) -> XrayNode {
        XrayNode {
            id: "test".into(),
            subscription_id: "sub".into(),
            name: "Test".into(),
            country_code: String::new(),
            country_name: String::new(),
            protocol: Protocol::Unknown,
            address: String::new(),
            port: 0,
            raw_config: raw.into(),
        }
    }

    #[test]
    fn matches_xray_rs_socks_direct_example() {
        let config = generate_config(
            &node("vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443"),
            1080,
            InboundProtocol::Socks,
        )
        .unwrap();
        assert_eq!(config["inbounds"][0]["tag"], "socks-in");
        assert_eq!(config["inbounds"][0]["protocol"], "socks");
        assert_eq!(config["inbounds"][0]["listen"], "127.0.0.1");
        assert_eq!(config["inbounds"][0]["port"], 1080);
        assert_eq!(config["inbounds"][0]["settings"]["auth"], "noauth");
        assert_eq!(config["inbounds"][0]["settings"]["udp"], true);
        assert_eq!(config["outbounds"][0]["protocol"], "vless");
        assert_eq!(
            config["outbounds"][0]["settings"]["vnext"][0]["users"][0]["id"],
            "00010203-0405-0607-0809-0a0b0c0d0e0f"
        );
        assert_eq!(config["routing"]["domainStrategy"], "AsIs");
        assert_eq!(config["routing"]["rules"].as_array().unwrap().len(), 0);
    }

    #[test]
    fn builds_settings_for_every_protocol() {
        let cases = [
            "vmess://00112233-4455-6677-8899-aabbccddeeff@vm.example:443",
            "trojan://secret@tr.example:443",
            "ss://YWVzLTEyOC1nY206cGFzc3dvcmQ@ss.example:8388",
            "socks://user:pass@[2001:db8::7]:1080#Test",
            "https://user:pass@proxy.example:443",
        ];
        for raw in cases {
            let config = generate_config(&node(raw), 2080, InboundProtocol::Http).unwrap();
            let outbound = &config["outbounds"][0];
            assert!(!outbound["protocol"].as_str().unwrap().is_empty(), "{raw}");
            assert!(outbound["settings"].is_object(), "{raw}");
            assert_eq!(config["inbounds"][0]["protocol"], "http");
            assert_eq!(config["inbounds"][0]["port"], 2080);
        }
    }
}
