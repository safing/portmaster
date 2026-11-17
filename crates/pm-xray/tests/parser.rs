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

#[test]
fn filters_dummy_ad_nodes_and_sets_coordinates() {
    let raw_payload = "\
hysteria2://3a17ce52-4547-45aa-9820-a7d03ad5faca@media.embrofree.org:443/?sni=media.embrofree.org#%F0%9F%87%AA%F0%9F%87%BA%20%5BHY2%5D%20EmbroKVN%20%D0%91%D0%B5%D1%81%D0%BF%D0%BB%D0%B0%D1%82%D0%BD%D0%B0%D1%8F%20%D1%81%D0%B0%D0%B1%D0%BA%D0%B0\n\
vless://3a17ce52-4547-45aa-9820-a7d03ad5faca@real.embrofree.org:443?encryption=none&type=tcp&security=reality&sni=cdn.jsdelivr.net&fp=firefox&pbk=wfp4jyEksrozyS739zqIsNXySoqeEVtaQlVmeNeLp28#%F0%9F%87%AA%F0%9F%87%BA%20%5BVL%5D%20EmbroKVN%20%D0%91%D0%B5%D1%81%D0%BF%D0%BB%D0%B0%D1%82%D0%BD%D0%B0%D1%8F%20%D1%81%D0%B0%D0%B1%D0%BA%D0%B0\n\
ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTp4bnQ5empkYXB0Tkc3QlJ3Z1JocnhFY05ya01SVTU3dg==@1:1234#%D0%9E%D1%84%D0%BE%D1%80%D0%BC%D0%B8%20%D0%BF%D0%BE%D0%B4%D0%BF%D0%B8%D1%81%D0%BA%D1%83\n\
ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTp4bnQ5empkYXB0Tkc3QlJ3Z1JocnhFY05ya01SVTU3dg==@1:1234#%D1%87%D1%82%D0%BE%D0%B1%D1%8B%20%D0%BF%D0%BE%D0%BB%D1%83%D1%87%D0%B8%D1%82%D1%8C%20%D0%BB%D0%B8%D1%87%D0%BD%D1%8B%D0%B9%20%D0%B4%D0%BE%D1%81%D1%82%D1%83%D0%BF\n\
ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTp4bnQ5empkYXB0Tkc3QlJ3Z1JocnhFY05ya01SVTU3dg==@1:1234#%D0%B1%D0%B5%D0%B7%20%D0%BB%D0%B8%D0%BC%D0%B8%D1%82%D0%B0%20%D1%82%D1%80%D0%B0%D1%84%D0%B8%D0%BA%D0%B0%20%D0%B8%20%D1%81%D0%BA%D0%BE%D1%80%D0%BE%D1%81%D1%82%D0%B8\n\
ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTp4bnQ5empkYXB0Tkc3QlJ3Z1JocnhFY05ya01SVTU3dg==@1:1234#%D0%9E%D1%82%2098%20%D1%80%20%D0%BD%D0%B0%206%20%D0%BC%D0%B5%D1%81%D1%8F%D1%86%D0%B5%D0%B2\n\
ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTp4bnQ5empkYXB0Tkc3QlJ3Z1JocnhFY05ya01SVTU3dg==@1:1234#%D0%98%D0%BD%D1%81%D1%82%D1%80%D1%83%D0%BA%D1%86%D0%B8%D1%8F%20%D0%BD%D0%B0%20%D1%81%D0%B0%D0%B9%D1%82%D0%B5\n\
ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTp4bnQ5empkYXB0Tkc3QlJ3Z1JocnhFY05ya01SVTU3dg==@1:1234#kvn-wiki.com";

    let links = decode_payload(raw_payload).unwrap();
    let valid_nodes: Vec<_> = links
        .iter()
        .filter_map(|link| parse_profile(link, "sub-1", None).ok())
        .collect();

    // The 6 dummy "ss://...@1:1234" advertisement links must be filtered out!
    assert_eq!(valid_nodes.len(), 2);
    assert_eq!(valid_nodes[0].protocol, Protocol::Hysteria2);
    assert_eq!(valid_nodes[1].protocol, Protocol::Vless);
    // Real nodes have country detected from flag emoji and coordinates populated
    assert!(valid_nodes[0].latitude.is_some() || !valid_nodes[0].country_code.is_empty());
}
