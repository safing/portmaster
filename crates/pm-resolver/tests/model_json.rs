use std::net::IpAddr;

use pm_resolver::*;

#[test]
fn test_resolver_info_json() {
    let info = ResolverInfo {
        name: "Cloudflare".to_string(),
        r#type: "dot".to_string(),
        source: "config".to_string(),
        ip: Some("1.1.1.1".parse::<IpAddr>().unwrap()),
        domain: "cloudflare-dns.com".to_string(),
        ip_scope: IpScope::Global,
        port: 853,
    };

    let serialized = pm_dsd::gojson::to_vec(&info).unwrap();
    let json_str = String::from_utf8(serialized).unwrap();
    let expected = r#"{"Name":"Cloudflare","Type":"dot","Source":"config","IP":"1.1.1.1","Domain":"cloudflare-dns.com","IPScope":4,"Port":853}"#;
    assert_eq!(json_str, expected);

    // Roundtrip
    let deserialized: ResolverInfo = serde_json::from_str(&json_str).unwrap();
    assert_eq!(deserialized, info);
}

#[test]
fn test_resolver_info_empty_ip_json() {
    let info = ResolverInfo {
        name: String::new(),
        r#type: "dns".to_string(),
        source: String::new(),
        ip: None,
        domain: String::new(),
        ip_scope: IpScope::Global,
        port: 0,
    };

    let serialized = pm_dsd::gojson::to_vec(&info).unwrap();
    let json_str = String::from_utf8(serialized).unwrap();
    let expected = r#"{"Name":"","Type":"dns","Source":"","IP":"","Domain":"","IPScope":4,"Port":0}"#;
    assert_eq!(json_str, expected);

    // Roundtrip
    let deserialized: ResolverInfo = serde_json::from_str(&json_str).unwrap();
    assert_eq!(deserialized, info);
}

#[test]
fn test_resolver_json() {
    let r = Resolver {
        config_url: "dot://cloudflare-dns.com?ip=1.1.1.2&name=Cloudflare&blockedif=zeroip".to_string(),
        info: ResolverInfo {
            name: "Cloudflare".to_string(),
            r#type: "dot".to_string(),
            source: "config".to_string(),
            ip: Some("1.1.1.2".parse().unwrap()),
            domain: "cloudflare-dns.com".to_string(),
            ip_scope: IpScope::Global,
            port: 853,
        },
        server_address: "1.1.1.2:853".to_string(),
        upstream_block_detection: "zeroip".to_string(),
        search: None,
        search_only: false,
        path: String::new(),
        link_local_unavailable: false,
        conn: None,
    };

    let serialized = pm_dsd::gojson::to_vec(&r).unwrap();
    let json_str = String::from_utf8(serialized).unwrap();
    let expected = "{\"ConfigURL\":\"dot://cloudflare-dns.com?ip=1.1.1.2\\u0026name=Cloudflare\\u0026blockedif=zeroip\",\"Info\":{\"Name\":\"Cloudflare\",\"Type\":\"dot\",\"Source\":\"config\",\"IP\":\"1.1.1.2\",\"Domain\":\"cloudflare-dns.com\",\"IPScope\":4,\"Port\":853},\"ServerAddress\":\"1.1.1.2:853\",\"UpstreamBlockDetection\":\"zeroip\",\"Search\":null,\"SearchOnly\":false,\"Path\":\"\",\"LinkLocalUnavailable\":false}";
    assert_eq!(json_str, expected);

    // Roundtrip
    let deserialized: Resolver = serde_json::from_str(&json_str).unwrap();
    assert_eq!(deserialized.config_url, r.config_url);
    assert_eq!(deserialized.info, r.info);
    assert_eq!(deserialized.server_address, r.server_address);
    assert_eq!(deserialized.upstream_block_detection, r.upstream_block_detection);
    assert_eq!(deserialized.search, r.search);
    assert_eq!(deserialized.search_only, r.search_only);
    assert_eq!(deserialized.path, r.path);
    assert_eq!(deserialized.link_local_unavailable, r.link_local_unavailable);
}
