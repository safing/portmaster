//! Parser tests on realistic subscription fixtures.

use pm_xray::parser::{country_from_name, parse_profile};
use pm_xray::subscription::decode_payload;
use pm_xray::Protocol;

const PLAIN: &str = include_str!("fixtures/subscription.txt");
const BASE64: &str = include_str!("fixtures/subscription.b64");
const VMESS_JSON: &str = include_str!("fixtures/vmess.json.b64");

#[test]
fn decodes_base64_and_raw_subscriptions() {
    let plain = decode_payload(PLAIN).unwrap();
    assert_eq!(plain.len(), 6);
    assert_eq!(decode_payload(BASE64).unwrap(), plain);
    assert!(decode_payload("not a subscription").is_err());
}

#[test]
fn parses_all_profile_kinds() {
    let links = decode_payload(PLAIN).unwrap();
    let nodes: Vec<_> = links
        .iter()
        .map(|link| parse_profile(link, "sub-1", None).unwrap())
        .collect();
    assert_eq!(nodes.len(), 6);

    let vless = &nodes[0];
    assert_eq!(vless.protocol, Protocol::Vless);
    assert_eq!(vless.address, "vless.example.com");
    assert_eq!(vless.port, 443);
    assert_eq!(vless.country_code, "DE");
    assert!(vless.raw_config.contains("type=ws"));

    assert_eq!(nodes[1].protocol, Protocol::Trojan);
    assert_eq!(nodes[1].country_code, "US");
    assert_eq!(nodes[2].protocol, Protocol::Shadowsocks);
    assert_eq!(nodes[2].country_code, "FR");
    assert_eq!(nodes[3].protocol, Protocol::Vmess);
    assert_eq!(nodes[3].country_code, "RU");
    assert_eq!(nodes[4].protocol, Protocol::Socks);
    assert_eq!(nodes[4].port, 1080);
    assert_eq!(nodes[5].protocol, Protocol::Http);
    assert_eq!(nodes[5].port, 443);

    // Node IDs are stable and scoped to the subscription.
    assert_ne!(nodes[0].id, nodes[1].id);
    assert_eq!(nodes[0].id, parse_profile(&links[0], "sub-1", None).unwrap().id);
    assert_ne!(nodes[0].id, parse_profile(&links[0], "sub-2", None).unwrap().id);
}

#[test]
fn parses_vmess_base64_json() {
    let raw = format!("vmess://{}", VMESS_JSON.trim());
    let node = parse_profile(&raw, "sub-1", None).unwrap();
    assert_eq!(node.protocol, Protocol::Vmess);
    assert_eq!(node.name, "JP-VMESS-JSON");
    assert_eq!(node.country_code, "JP");
    assert_eq!(node.address, "vm.example.net");
    assert_eq!(node.port, 443);
}

#[test]
fn detects_flags_bracket_codes_and_names() {
    assert_eq!(country_from_name("Node \u{1F1F7}\u{1F1FA} 01").unwrap().0, "RU");
    assert_eq!(country_from_name("[NL] Amsterdam").unwrap().0, "NL");
    assert_eq!(country_from_name("Frankfurt, Germany #2").unwrap().0, "DE");
    assert!(country_from_name("Mystery Node").is_none());
}

#[test]
fn accepts_vmess_json_with_string_port() {
    use base64::{engine::general_purpose::STANDARD, Engine};
    let json = r#"{"v":"2","ps":"DE String Port","add":"de.example.net","port":"443","id":"00010203-0405-0607-0809-0a0b0c0d0e0f","net":"ws","type":"none"}"#;
    let raw = format!("vmess://{}", STANDARD.encode(json));
    let node = parse_profile(&raw, "sub-1", None).unwrap();
    assert_eq!(node.port, 443);
    assert_eq!(node.country_code, "DE");
}

#[test]
fn percent_decode_does_not_panic_on_multibyte_characters() {
    // Invalid percent escape followed by a 4-byte emoji. The old byte-index
    // slice into &str panicked here ("not a char boundary").
    let raw = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443#%😀";
    let node = parse_profile(raw, "sub-1", None).unwrap();
    assert_eq!(node.port, 443);
}
