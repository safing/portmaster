//! Config generator tests; structures are compared with the canonical
//! xray-rs examples (`config example socks-direct`, `vless-tls` and the
//! vmess/trojan/shadowsocks fixtures).

use pm_xray::config::{generate_config, generate_config_with_stats, write_config, InboundProtocol};
use pm_xray::{Protocol, XrayNode};

fn node(raw: &str) -> XrayNode {
    XrayNode {
        id: "node-1".into(),
        subscription_id: "sub-1".into(),
        name: "Test".into(),
        country_code: "DE".into(),
        country_name: "Germany".into(),
        protocol: Protocol::Unknown,
        address: String::new(),
        port: 0,
        raw_config: raw.into(),
    }
}

#[test]
fn config_matches_socks_direct_and_vless_tls_examples() {
    let raw = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@vless.example.com:443?type=ws&security=tls&path=%2Fws#DE-VLESS-01";
    let config = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();

    // `config example socks-direct`: inbound shape.
    assert_eq!(config["inbounds"][0]["tag"], "socks-in");
    assert_eq!(config["inbounds"][0]["protocol"], "socks");
    assert_eq!(config["inbounds"][0]["listen"], "127.0.0.1");
    assert_eq!(config["inbounds"][0]["port"], 1080);
    assert_eq!(config["inbounds"][0]["settings"]["auth"], "noauth");
    assert_eq!(config["inbounds"][0]["settings"]["udp"], true);

    // `config example vless-tls`: outbound + stream shape.
    let outbound = &config["outbounds"][0];
    assert_eq!(outbound["tag"], "proxy");
    assert_eq!(outbound["protocol"], "vless");
    assert_eq!(
        outbound["settings"]["vnext"][0]["address"],
        "vless.example.com"
    );
    assert_eq!(outbound["settings"]["vnext"][0]["port"], 443);
    assert_eq!(
        outbound["settings"]["vnext"][0]["users"][0]["id"],
        "00010203-0405-0607-0809-0a0b0c0d0e0f"
    );
    assert_eq!(outbound["settings"]["vnext"][0]["users"][0]["encryption"], "none");
    assert_eq!(outbound["streamSettings"]["network"], "ws");
    assert_eq!(outbound["streamSettings"]["security"], "tls");
    assert_eq!(
        outbound["streamSettings"]["tlsSettings"]["serverName"],
        "vless.example.com"
    );
    assert_eq!(outbound["streamSettings"]["wsSettings"]["path"], "/ws");
    assert_eq!(config["routing"]["domainStrategy"], "AsIs");
    assert_eq!(config["routing"]["rules"].as_array().unwrap().len(), 0);
}

#[test]
fn config_matches_protocol_fixtures() {
    // Flattened VMess settings, as in xray-rs tests/fixtures/configs/vmess.json.
    let raw = "vmess://00112233-4455-6677-8899-aabbccddeeff@vm.example:443";
    let config = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();
    let outbound = &config["outbounds"][0];
    assert_eq!(outbound["protocol"], "vmess");
    assert_eq!(outbound["settings"]["address"], "vm.example");
    assert_eq!(outbound["settings"]["id"], "00112233-4455-6677-8899-aabbccddeeff");
    assert_eq!(outbound["settings"]["security"], "auto");

    // Trojan with TLS, as in the trojan fixture.
    let raw = "trojan://synthetic-secret@trojan.example:443";
    let config = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();
    let outbound = &config["outbounds"][0];
    assert_eq!(outbound["protocol"], "trojan");
    assert_eq!(outbound["settings"]["password"], "synthetic-secret");
    assert_eq!(outbound["streamSettings"]["security"], "tls");
    assert_eq!(
        outbound["streamSettings"]["tlsSettings"]["serverName"],
        "trojan.example"
    );

    // Shadowsocks, as in the shadowsocks2022 fixture (method/password flat).
    let raw = "ss://YWVzLTEyOC1nY206cGFzc3dvcmQ@ss.example:8388";
    let config = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();
    let outbound = &config["outbounds"][0];
    assert_eq!(outbound["protocol"], "shadowsocks");
    assert_eq!(outbound["settings"]["method"], "aes-128-gcm");
    assert_eq!(outbound["settings"]["password"], "password");
}

#[test]
fn config_exposes_stats_api_when_port_is_set() {
    let raw = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@vless.example.com:443";
    let config = generate_config_with_stats(&node(raw), 1080, InboundProtocol::Socks, 62789)
        .unwrap();
    assert_eq!(config["api"]["tag"], "api");
    assert_eq!(config["api"]["services"][0], "StatsService");
    assert_eq!(config["stats"], serde_json::json!({}));
    assert_eq!(config["policy"]["system"]["statsOutboundUplink"], true);
    assert_eq!(config["policy"]["system"]["statsOutboundDownlink"], true);
    let inbounds = config["inbounds"].as_array().unwrap();
    assert_eq!(inbounds.len(), 2);
    assert_eq!(inbounds[1]["tag"], "api");
    assert_eq!(inbounds[1]["port"], 62789);
    assert_eq!(inbounds[1]["protocol"], "tunnel");

    let plain = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();
    assert!(plain.get("api").is_none());
    assert_eq!(plain["inbounds"].as_array().unwrap().len(), 1);
}

#[test]
fn config_supports_http_inbound_and_writes_to_disk() {
    let raw = "https://user:pass@proxy.example:443";
    let config = generate_config(&node(raw), 2080, InboundProtocol::Http).unwrap();
    assert_eq!(config["inbounds"][0]["protocol"], "http");
    assert_eq!(config["inbounds"][0]["port"], 2080);
    assert_eq!(config["outbounds"][0]["protocol"], "http");
    assert_eq!(config["outbounds"][0]["settings"]["user"], "user");
    assert_eq!(config["outbounds"][0]["settings"]["pass"], "pass");
    assert_eq!(config["outbounds"][0]["streamSettings"]["security"], "tls");

    let dir = std::env::temp_dir().join(format!("pm-xray-config-test-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&dir);
    let path = write_config(&dir, &node(raw), 2080, InboundProtocol::Http).unwrap();
    assert!(path.exists());
    let written: serde_json::Value =
        serde_json::from_slice(&std::fs::read(&path).unwrap()).unwrap();
    assert_eq!(written, config);
    let _ = std::fs::remove_dir_all(&dir);
}
