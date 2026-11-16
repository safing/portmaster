//! Extended share-link schemes: fixtures and generated outbound structure.

use pm_xray::config::{generate_config, InboundProtocol};
use pm_xray::parser::parse_profile;
use pm_xray::subscription::decode_payload;
use pm_xray::{Protocol, XrayNode};

const EXTENDED: &str = include_str!("fixtures/extended.txt");

fn node(raw: &str) -> XrayNode {
    XrayNode {
        id: "ext-1".into(),
        subscription_id: "sub-1".into(),
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
fn parses_every_extended_scheme() {
    let links = decode_payload(EXTENDED).unwrap();
    assert_eq!(links.len(), 7, "fixture has 7 profiles");

    let nodes: Vec<_> = links
        .iter()
        .map(|link| parse_profile(link, "sub-1", None).unwrap())
        .collect();

    assert_eq!(nodes[0].protocol, Protocol::Hysteria2);
    assert_eq!(nodes[0].address, "hy.example.com");
    assert_eq!(nodes[0].port, 443);
    assert_eq!(nodes[0].country_code, "DE");

    assert_eq!(nodes[1].protocol, Protocol::Hysteria2);
    assert_eq!(nodes[1].address, "2001:db8::9");
    assert_eq!(nodes[1].port, 8443);

    assert_eq!(nodes[2].protocol, Protocol::Shadowsocks);
    assert_eq!(nodes[3].protocol, Protocol::Shadowsocks);

    assert_eq!(nodes[4].protocol, Protocol::Wireguard);
    assert_eq!(nodes[4].address, "wg.example.com");
    assert_eq!(nodes[4].port, 51820);

    assert_eq!(nodes[5].protocol, Protocol::Vless);
    // An unknown query parameter must not break parsing.
    assert_eq!(nodes[5].name, "RU-VLESS-EXT");

    assert_eq!(nodes[6].protocol, Protocol::Trojan);
}

#[test]
fn hysteria2_outbound_matches_xray_rs_shape() {
    let links = decode_payload(EXTENDED).unwrap();
    let config = generate_config(&node(&links[0]), 1080, InboundProtocol::Socks).unwrap();
    let outbound = &config["outbounds"][0];
    assert_eq!(outbound["protocol"], "hysteria");
    assert_eq!(outbound["settings"]["version"], 2);
    assert_eq!(outbound["settings"]["address"], "hy.example.com");
    assert_eq!(outbound["settings"]["port"], 443);

    let stream = &outbound["streamSettings"];
    assert_eq!(stream["network"], "hysteria");
    assert_eq!(stream["security"], "tls");
    assert_eq!(stream["tlsSettings"]["serverName"], "hy.example.com");
    assert_eq!(stream["tlsSettings"]["alpn"], serde_json::json!(["h3"]));
    assert_eq!(stream["hysteriaSettings"]["version"], 2);
    assert_eq!(stream["hysteriaSettings"]["auth"], "aHVudGVyMjA5");
    assert_eq!(
        stream["finalmask"]["udp"][0]["type"],
        "salamander"
    );
    assert_eq!(
        stream["finalmask"]["udp"][0]["settings"]["password"],
        "salamander-secret"
    );
    assert_eq!(stream["finalmask"]["udp"][1]["type"], "udphop");
    assert_eq!(
        stream["finalmask"]["udp"][1]["settings"]["remotePorts"],
        "1000-2000"
    );

    // A certificate pin is passed through to tlsSettings.
    let pinned = generate_config(&node(&links[1]), 1080, InboundProtocol::Socks).unwrap();
    assert_eq!(
        pinned["outbounds"][0]["streamSettings"]["tlsSettings"]["pinnedPeerCertSha256"],
        "ee55e95dabd185916504c46d990a4291fb939be93c9fd4e4e0ad3daa9fa0a46f"
    );
}

#[test]
fn shadowsocks_plugin_options_are_preserved() {
    let links = decode_payload(EXTENDED).unwrap();
    let obfs = generate_config(&node(&links[2]), 1080, InboundProtocol::Socks).unwrap();
    assert_eq!(obfs["outbounds"][0]["settings"]["plugin"], "obfs-local");
    assert_eq!(
        obfs["outbounds"][0]["settings"]["pluginOpts"],
        "obfs=http;obfs-host=www.example.com"
    );
    let v2 = generate_config(&node(&links[3]), 1080, InboundProtocol::Socks).unwrap();
    assert_eq!(v2["outbounds"][0]["settings"]["plugin"], "v2ray-plugin");
    assert_eq!(
        v2["outbounds"][0]["settings"]["pluginOpts"],
        "mode=websocket;path=/ws;host=cdn.example.com;tls"
    );
}

#[test]
fn wireguard_outbound_matches_xray_rs_shape() {
    let links = decode_payload(EXTENDED).unwrap();
    let config = generate_config(&node(&links[4]), 1080, InboundProtocol::Socks).unwrap();
    assert_eq!(config["outbounds"][0]["protocol"], "freedom");
    let outbound = &config["outbounds"][1];
    assert_eq!(outbound["protocol"], "wireguard");
    let settings = &outbound["settings"];
    assert_eq!(settings["secretKey"], "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=");
    assert_eq!(settings["address"], serde_json::json!(["10.0.0.2/32", "fd00::2/128"]));
    assert_eq!(settings["mtu"], 1420);
    assert_eq!(settings["remoteDNS"], serde_json::json!(["1.1.1.1", "2606:4700:4700::1111"]));
    assert_eq!(settings["peers"][0]["publicKey"], "ISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0+P0A=");
    assert_eq!(settings["peers"][0]["endpoint"], "wg.example.com:51820");
    assert_eq!(settings["peers"][0]["keepAlive"], 25);
    assert_eq!(
        settings["peers"][0]["allowedIPs"],
        serde_json::json!(["0.0.0.0/0", "::/0"])
    );
    assert_eq!(config["routing"]["domainStrategy"], "IPOnDemand");
    assert_eq!(config["dns"]["servers"][0], "1.1.1.1");
    assert!(outbound["streamSettings"].is_null());
}

#[test]
fn custom_http_headers_and_header_type_reach_stream_settings() {
    let raw = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@vless.example.com:443?type=ws&path=%2Fws&host=cdn.example.com&security=tls&headers=%7B%22User-Agent%22%3A%22portmaster%22%2C%22X-Test%22%3A%22yes%22%2C%22Host%22%3A%22ignored%22%7D#DE-WS";
    let config = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();
    let ws = &config["outbounds"][0]["streamSettings"]["wsSettings"];
    assert_eq!(ws["headers"]["User-Agent"], "portmaster");
    assert_eq!(ws["headers"]["X-Test"], "yes");
    assert!(ws["headers"]["Host"].is_null());

    // Unsupported RAW header types are parsed but not emitted, to keep the
    // config acceptable to xray-rs.
    let raw = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@vless.example.com:443?type=tcp&headerType=http#DE-RAW";
    let config = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();
    assert!(config["outbounds"][0]["streamSettings"]["rawSettings"].is_null());

    let raw = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@vless.example.com:443?type=tcp&headerType=none#DE-RAW";
    let config = generate_config(&node(raw), 1080, InboundProtocol::Socks).unwrap();
    assert_eq!(
        config["outbounds"][0]["streamSettings"]["rawSettings"]["header"]["type"],
        "none"
    );
}

#[test]
fn extra_transport_parameters_reach_the_outbound() {
    let links = decode_payload(EXTENDED).unwrap();

    // VLESS: flow + gRPC serviceName + custom header (+ ignored unknown key).
    let config = generate_config(&node(&links[5]), 1080, InboundProtocol::Socks).unwrap();
    let outbound = &config["outbounds"][0];
    assert_eq!(
        outbound["settings"]["vnext"][0]["users"][0]["flow"],
        "xtls-rprx-vision"
    );
    assert_eq!(
        outbound["streamSettings"]["grpcSettings"]["serviceName"],
        "GunService"
    );
    assert_eq!(outbound["streamSettings"]["tlsSettings"]["fingerprint"], "chrome");
    assert_eq!(
        outbound["streamSettings"]["tlsSettings"]["alpn"],
        serde_json::json!(["h2"])
    );

    // Trojan: custom WebSocket path/host. `flow` is parsed but deliberately
    // not emitted, because xray-rs accepts an empty Trojan flow only.
    let config = generate_config(&node(&links[6]), 1080, InboundProtocol::Socks).unwrap();
    let outbound = &config["outbounds"][0];
    assert!(outbound["settings"]["flow"].is_null());
    assert_eq!(outbound["streamSettings"]["wsSettings"]["path"], "/tr");
    assert_eq!(outbound["streamSettings"]["wsSettings"]["host"], "cdn.example.com");
}
