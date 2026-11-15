use pm_resolver::*;

#[test]
fn test_valid_schemes_and_default_ports() {
    // Plain DNS
    let (res, skip) = create_resolver("dns://1.1.1.1", SERVER_SOURCE_CONFIGURED).unwrap();
    assert!(!skip);
    let r = res.unwrap();
    assert_eq!(r.info.r#type, SERVER_TYPE_DNS);
    assert_eq!(r.info.port, 53);
    assert_eq!(r.server_address, "1.1.1.1:53");

    // Plain TCP
    let (res, skip) = create_resolver("tcp://1.1.1.1", SERVER_SOURCE_CONFIGURED).unwrap();
    assert!(!skip);
    let r = res.unwrap();
    assert_eq!(r.info.r#type, SERVER_TYPE_TCP);
    assert_eq!(r.info.port, 53);
    assert_eq!(r.server_address, "1.1.1.1:53");

    // DoT
    let (res, skip) = create_resolver(
        "dot://cloudflare-dns.com?ip=1.1.1.1",
        SERVER_SOURCE_CONFIGURED,
    )
    .unwrap();
    assert!(!skip);
    let r = res.unwrap();
    assert_eq!(r.info.r#type, SERVER_TYPE_DOT);
    assert_eq!(r.info.port, 853);
    assert_eq!(r.info.domain, "cloudflare-dns.com");
    assert_eq!(r.server_address, "1.1.1.1:853");

    // DoH
    let (res, skip) =
        create_resolver("doh://cloudflare-dns.com/dns-query", SERVER_SOURCE_CONFIGURED).unwrap();
    assert!(!skip);
    let r = res.unwrap();
    assert_eq!(r.info.r#type, SERVER_TYPE_DOH);
    assert_eq!(r.info.port, 443);
    assert_eq!(r.path, "/dns-query");
    assert_eq!(r.server_address, "cloudflare-dns.com:443");

    // Legacy alias: tls -> dot
    let (res, skip) =
        create_resolver("tls://cloudflare-dns.com?ip=1.1.1.1", SERVER_SOURCE_CONFIGURED).unwrap();
    assert!(!skip);
    let r = res.unwrap();
    assert_eq!(r.info.r#type, SERVER_TYPE_DOT);
    assert_eq!(r.info.port, 853);

    // Legacy alias: https -> doh
    let (res, skip) =
        create_resolver("https://cloudflare-dns.com/dns-query", SERVER_SOURCE_CONFIGURED).unwrap();
    assert!(!skip);
    let r = res.unwrap();
    assert_eq!(r.info.r#type, SERVER_TYPE_DOH);
    assert_eq!(r.info.port, 443);
}

#[test]
fn test_custom_ports() {
    let (res, _) = create_resolver("dns://1.1.1.1:5353", SERVER_SOURCE_CONFIGURED).unwrap();
    assert_eq!(res.unwrap().info.port, 5353);

    let (res, _) = create_resolver("tcp://1.1.1.1:8053", SERVER_SOURCE_CONFIGURED).unwrap();
    assert_eq!(res.unwrap().info.port, 8053);

    let (res, _) = create_resolver(
        "dot://cloudflare-dns.com:8530?ip=1.1.1.1",
        SERVER_SOURCE_CONFIGURED,
    )
    .unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.port, 8530);
    assert_eq!(r.server_address, "1.1.1.1:8530");

    let (res, _) = create_resolver(
        "doh://cloudflare-dns.com:8443/dns-query",
        SERVER_SOURCE_CONFIGURED,
    )
    .unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.port, 8443);
    assert_eq!(r.server_address, "cloudflare-dns.com:8443");
}

#[test]
fn test_query_parameters() {
    let url = "dot://1.1.1.2:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip";
    let (res, _) = create_resolver(url, SERVER_SOURCE_CONFIGURED).unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.name, "Cloudflare");
    assert_eq!(r.info.domain, "cloudflare-dns.com");
    assert_eq!(r.upstream_block_detection, BLOCK_DETECTION_ZERO_IP);
    assert_eq!(r.server_address, "1.1.1.2:853");
    assert_eq!(
        r.info.descriptive_name(),
        "Cloudflare (dot://cloudflare-dns.com:853#config)"
    );

    // Blocked if options
    for (val, expected) in [
        ("empty", BLOCK_DETECTION_EMPTY_ANSWER),
        ("refused", BLOCK_DETECTION_REFUSED),
        ("zeroip", BLOCK_DETECTION_ZERO_IP),
        ("disabled", BLOCK_DETECTION_DISABLED),
    ] {
        let (res, _) = create_resolver(
            &format!("dns://1.1.1.1?blockedif={}", val),
            SERVER_SOURCE_CONFIGURED,
        )
        .unwrap();
        assert_eq!(res.unwrap().upstream_block_detection, expected);
    }

    // Default blockedif is zeroip
    let (res, _) = create_resolver("dns://1.1.1.1", SERVER_SOURCE_CONFIGURED).unwrap();
    assert_eq!(
        res.unwrap().upstream_block_detection,
        BLOCK_DETECTION_ZERO_IP
    );

    // Search domains and search-only
    let (res, _) = create_resolver(
        "dns://1.1.1.1?search=lan,home&search-only",
        SERVER_SOURCE_CONFIGURED,
    )
    .unwrap();
    let r = res.unwrap();
    assert_eq!(
        r.search,
        Some(vec![".lan.".to_string(), ".home.".to_string()])
    );
    assert!(r.search_only);

    // Link-local
    // Link-local: no check injected assumes available.
    let (res, _) = create_resolver("dns://1.1.1.1?link-local", SERVER_SOURCE_CONFIGURED).unwrap();
    assert!(!res.unwrap().link_local_unavailable);
}

#[test]
fn test_link_local_injected_check() {
    let none_local: &dyn Fn(std::net::IpAddr) -> bool = &|_| false;
    let all_local: &dyn Fn(std::net::IpAddr) -> bool = &|_| true;
    let url = "dns://192.168.1.1?link-local";

    let (res, _) = create_resolver_with_local_net(url, SERVER_SOURCE_CONFIGURED, Some(none_local)).unwrap();
    assert!(res.unwrap().link_local_unavailable);
    let (res, _) = create_resolver_with_local_net(url, SERVER_SOURCE_CONFIGURED, Some(all_local)).unwrap();
    assert!(!res.unwrap().link_local_unavailable);
    let (res, _) = create_resolver_with_local_net(url, SERVER_SOURCE_CONFIGURED, None).unwrap();
    assert!(!res.unwrap().link_local_unavailable);
    // Without the parameter the check is not used.
    let (res, _) =
        create_resolver_with_local_net("dns://192.168.1.1", SERVER_SOURCE_CONFIGURED, Some(none_local)).unwrap();
    assert!(!res.unwrap().link_local_unavailable);
}

#[test]
fn test_ipv6_bracketed_hosts() {
    let (res, _) = create_resolver("dns://[2001:db8::1]:53", SERVER_SOURCE_CONFIGURED).unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.ip, Some("2001:db8::1".parse().unwrap()));
    assert_eq!(r.info.port, 53);
    assert_eq!(r.server_address, "[2001:db8::1]:53");

    let (res, _) = create_resolver("dns://[2001:db8::1]", SERVER_SOURCE_CONFIGURED).unwrap();
    assert_eq!(res.unwrap().server_address, "[2001:db8::1]:53");

    let (res, _) =
        create_resolver("dot://[2001:db8::1]?verify=dns.example", SERVER_SOURCE_CONFIGURED).unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.domain, "dns.example");
    assert_eq!(r.info.ip, Some("2001:db8::1".parse().unwrap()));
    assert_eq!(r.info.port, 853);
    assert_eq!(r.server_address, "[2001:db8::1]:853");

    // ip= parameter with IPv6 value is bracketed like net.JoinHostPort.
    let (res, _) = create_resolver("dot://dns.example?ip=2001:db8::2", SERVER_SOURCE_CONFIGURED).unwrap();
    assert_eq!(res.unwrap().server_address, "[2001:db8::2]:853");
}

#[test]
fn test_https_alias_path_matches_go() {
    // Go u.Path is empty without a path, also for the https alias.
    for (url, path) in [
        ("https://dns.example", ""),
        ("https://dns.example/", "/"),
        ("https://dns.example?name=x", ""),
        ("doh://dns.example", ""),
        ("doh://dns.example/dns-query?name=x", "/dns-query"),
        ("https://dns.example/a%20b", "/a b"),
    ] {
        let (res, _) = create_resolver(url, SERVER_SOURCE_CONFIGURED).unwrap();
        assert_eq!(res.unwrap().path, path, "{}", url);
    }
}

#[test]
fn test_check_order_and_query_semantics() {
    // IP check precedes port check and params (Go order).
    let err = create_resolver("dns://example.com:abc?foo=1", SERVER_SOURCE_CONFIGURED).unwrap_err();
    assert!(err.to_string().contains("invalid port"), "{}", err);
    let err = create_resolver("dns://example.com?foo=1", SERVER_SOURCE_CONFIGURED).unwrap_err();
    assert_eq!(err.to_string(), "resolver IP \"example.com\" is invalid");

    // Repeated key: first value wins (url.Values.Get).
    let (res, _) =
        create_resolver("dns://1.1.1.1?name=first&name=second", SERVER_SOURCE_CONFIGURED).unwrap();
    assert_eq!(res.unwrap().info.name, "first");

    // Percent-decoded keys.
    let (res, _) = create_resolver("dns://1.1.1.1?n%61me=decoded", SERVER_SOURCE_CONFIGURED).unwrap();
    assert_eq!(res.unwrap().info.name, "decoded");
    let err = create_resolver("dns://1.1.1.1?fo%6f=1", SERVER_SOURCE_CONFIGURED).unwrap_err();
    assert_eq!(err.to_string(), "unknown parameter \"\"foo\"\"");
}

#[test]
fn test_ipv4_mapped_ip_canonicalized() {
    let (res, _) = create_resolver("dns://[::ffff:1.2.3.4]", SERVER_SOURCE_CONFIGURED).unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.ip, Some("1.2.3.4".parse().unwrap()));
    assert_eq!(r.server_address, "1.2.3.4:53");
    assert_eq!(r.info.id(), "dns://1.2.3.4:53#config");
}

#[test]
fn test_dot_doh_domain_ip_combinations() {
    // Domain and ip parameter
    let (res, _) = create_resolver(
        "dot://cloudflare-dns.com?ip=1.1.1.1",
        SERVER_SOURCE_CONFIGURED,
    )
    .unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.domain, "cloudflare-dns.com");
    assert_eq!(r.info.ip, Some("1.1.1.1".parse().unwrap()));
    assert_eq!(r.server_address, "1.1.1.1:853");

    // IP and verify parameter
    let (res, _) = create_resolver(
        "dot://1.1.1.1?verify=cloudflare-dns.com",
        SERVER_SOURCE_CONFIGURED,
    )
    .unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.domain, "cloudflare-dns.com");
    assert_eq!(r.info.ip, Some("1.1.1.1".parse().unwrap()));
    assert_eq!(r.server_address, "1.1.1.1:853");

    // Domain only (no IP)
    let (res, _) =
        create_resolver("dot://cloudflare-dns.com", SERVER_SOURCE_CONFIGURED).unwrap();
    let r = res.unwrap();
    assert_eq!(r.info.domain, "cloudflare-dns.com");
    assert_eq!(r.info.ip, None);
    assert_eq!(r.server_address, "cloudflare-dns.com:853");
}

#[test]
fn test_validation_errors() {
    let cases = vec![
        ("http://1.1.1.1", "DNS resolver scheme \"http\" invalid"),
        ("dns://google.com", "resolver IP \"google.com\" is invalid"),
        ("tcp://google.com", "resolver IP \"google.com\" is invalid"),
        (
            "dns://1.1.1.1?verify=google.com",
            "domain verification is only supported by DoT and DoH servers",
        ),
        (
            "tcp://1.1.1.1?verify=google.com",
            "domain verification is only supported by DoT and DoH servers",
        ),
        (
            "dot://example.com?verify=example.com",
            "cannot set the domain name via both the hostname in the URL and the verify parameter",
        ),
        (
            "dot://1.1.1.1",
            "verify parameter must be set when using ip as domain",
        ),
        (
            "dot://1.1.1.1?verify=example.com&ip=1.0.0.1",
            "cannot set the IP address via both the hostname in the URL and the ip parameter",
        ),
        (
            "dns://1.1.1.1:abc",
            "parse \"dns://1.1.1.1:abc\": invalid port \":abc\" after host",
        ),
        ("dns://1.1.1.1:99999", "invalid port \"99999\""),
        (
            "dns://1.1.1.1?foo=bar",
            "unknown parameter \"\"foo\"\"",
        ),
        (
            "dns://1.1.1.1?blockedif=nxdomain",
            "invalid value for upstream block detection (blockedif=)",
        ),
        (
            "dns://1.1.1.1?search=lan&search-only=1",
            "search-only may only be used as an empty parameter",
        ),
        (
            "dns://1.1.1.1?search-only",
            "cannot use search-only without search scopes",
        ),
        (
            "dns://1.1.1.1?link-local=true",
            "link-local may only be used as an empty parameter",
        ),
    ];

    for (url, expected_err) in cases {
        let err = create_resolver(url, SERVER_SOURCE_CONFIGURED).unwrap_err();
        assert_eq!(err.to_string(), expected_err, "failed for url: {}", url);
    }
}

#[test]
fn test_search_scope_check() {
    // Invalid
    assert!(check_search_scope(".").is_err());
    assert!(check_search_scope(".com.").is_err());
    assert!(check_search_scope("com.").is_err());
    assert!(check_search_scope(".com").is_err());

    // Dangerously high
    assert!(check_search_scope("com").is_err());
    assert!(check_search_scope("net").is_err());
    assert!(check_search_scope("org").is_err());
    assert!(check_search_scope("pvt.k12.ma.us").is_err());

    // Succeeded
    assert!(check_search_scope("a.com").is_ok());
    assert!(check_search_scope("b.a.com").is_ok());
    assert!(check_search_scope("c.b.a.com").is_ok());
    assert!(check_search_scope("test.pvt.k12.ma.us").is_ok());

    assert!(check_search_scope("onion").is_ok());
    assert!(check_search_scope("a.onion").is_ok());
    assert!(check_search_scope("b.a.onion").is_ok());
    assert!(check_search_scope("c.b.a.onion").is_ok());

    assert!(check_search_scope("bit").is_ok());
    assert!(check_search_scope("a.bit").is_ok());
    assert!(check_search_scope("b.a.bit").is_ok());
    assert!(check_search_scope("c.b.a.bit").is_ok());

    assert!(check_search_scope("doesnotexist").is_ok());
    assert!(check_search_scope("a.doesnotexist").is_ok());
    assert!(check_search_scope("b.a.doesnotexist").is_ok());
    assert!(check_search_scope("c.b.a.doesnotexist").is_ok());
}

#[test]
fn test_operating_system_source_skips_localhost() {
    let (res, skip) = create_resolver("dns://127.0.0.1:53", SERVER_SOURCE_OPERATING_SYSTEM).unwrap();
    assert!(skip);
    assert!(res.is_none());

    let (res, skip) = create_resolver("dns://127.0.0.1:53", SERVER_SOURCE_CONFIGURED).unwrap();
    assert!(!skip);
    assert!(res.is_some());
}
