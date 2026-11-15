use std::collections::HashMap;
use std::net::IpAddr;

use url::{Host, Url};

use crate::error::ParseResolverError;
use crate::model::{
    IpScope, Resolver, ResolverInfo, BLOCK_DETECTION_DISABLED, BLOCK_DETECTION_EMPTY_ANSWER,
    BLOCK_DETECTION_REFUSED, BLOCK_DETECTION_ZERO_IP, HTTPS_PROTOCOL, SERVER_SOURCE_OPERATING_SYSTEM,
    SERVER_TYPE_DNS, SERVER_TYPE_DOH, SERVER_TYPE_DOT, SERVER_TYPE_TCP, TLS_PROTOCOL,
};

pub const PARAMETER_NAME: &str = "name";
pub const PARAMETER_VERIFY: &str = "verify";
pub const PARAMETER_IP: &str = "ip";
pub const PARAMETER_BLOCKED_IF: &str = "blockedif";
pub const PARAMETER_SEARCH: &str = "search";
pub const PARAMETER_SEARCH_ONLY: &str = "search-only";
pub const PARAMETER_LINK_LOCAL: &str = "link-local";

pub const MAX_SEARCH_DOMAINS: usize = 100;

pub const SPECIAL_SERVICE_DOMAINS: &[&str] = &[
    ".onion.",
    ".i2p.",
    ".loki.",
    ".bit.",
    ".eth.",
    ".888.",
    ".bitcoin.",
    ".coin.",
    ".crypto.",
    ".dao.",
    ".nft.",
    ".wallet.",
    ".x.",
    ".zil.",
    ".bazar.",
    ".emc.",
    ".lib.",
    ".bbs.",
    ".chan.",
    ".dyn.",
    ".free.",
    ".fur.",
    ".geek.",
    ".glue.",
    ".gopher.",
    ".indy.",
    ".libre.",
    ".neo.",
    ".null.",
    ".o.",
    ".oss.",
    ".oz.",
    ".parody.",
    ".pirate.",
    ".ku.",
    ".te.",
    ".ti.",
    ".uu.",
];

/// Formats an IP address and port into host:port (brackets IPv6 addresses).
pub fn format_ip_and_port(ip: IpAddr, port: u16) -> String {
    match ip {
        IpAddr::V4(v4) => format!("{}:{}", v4, port),
        IpAddr::V6(v6) => format!("[{}]:{}", v6, port),
    }
}

/// Formats a host and port into host:port (brackets IPv6 addresses).
pub fn format_host_and_port(host: &str, port: u16) -> String {
    // Go net.JoinHostPort: bracket anything containing a colon.
    if host.contains(':') {
        format!("[{}]:{}", host, port)
    } else {
        format!("{}:{}", host, port)
    }
}

/// Checks if a search domain is valid and not dangerously high up the hierarchy.
pub fn check_search_scope(search_domain: &str) -> Result<(), String> {
    // Sanity check the input.
    if search_domain.is_empty()
        || search_domain.starts_with('.')
        || search_domain.ends_with('.')
    {
        return Err(format!("invalid (1) search domain: {}", search_domain));
    }

    let dot_wrapped = format!(".{}.", search_domain);
    for scope in SPECIAL_SERVICE_DOMAINS {
        if dot_wrapped.ends_with(scope) {
            return Ok(());
        }
    }

    let augmented = format!("*.*.*.*.*.{}", search_domain);
    let (suffix, icann) = match psl::suffix(augmented.as_bytes()) {
        Some(s) => {
            let suffix_str = std::str::from_utf8(s.as_bytes()).unwrap_or("");
            let is_icann = s.typ() == Some(psl::Type::Icann);
            (suffix_str, is_icann)
        }
        None => {
            let last_label = search_domain.rsplit('.').next().unwrap_or(search_domain);
            (last_label, false)
        }
    };

    if suffix.is_empty() {
        return Err(format!("invalid (2) search domain: {}", search_domain));
    }

    // TLDs that are not managed by ICANN (ie. are unofficial) may be diverted completely.
    if !icann && !suffix.contains('.') {
        return Ok(());
    }

    let split = augmented.len() - suffix.len() - 1;
    let before_split = &augmented[..split];
    let last_dot = match before_split.rfind('.') {
        Some(idx) => idx,
        None => return Ok(()),
    };
    let etld_plus_1 = &augmented[last_dot + 1..];

    if etld_plus_1.contains('*') {
        return Err(format!(
            "search domain \"{}\" is dangerously high up the hierarchy, stay at or below \"{}\"",
            search_domain, etld_plus_1
        ));
    }

    Ok(())
}

/// Configures and validates search domains on a resolver.
pub fn configure_search_domains(
    resolver: &mut Resolver,
    searches: &[String],
    hardfail: bool,
) -> Result<(), ParseResolverError> {
    let mut search_list = Vec::with_capacity(searches.len());

    for (i, value) in searches.iter().enumerate() {
        let trimmed = value.trim_matches('.').to_lowercase();
        match check_search_scope(&trimmed) {
            Ok(()) => {
                search_list.push(format!(".{}.", trimmed));
            }
            Err(err) => {
                if hardfail {
                    resolver.search = None;
                    return Err(ParseResolverError::SearchDomainValidation(i + 1, err));
                }
            }
        }
    }

    if search_list.len() > MAX_SEARCH_DOMAINS {
        if hardfail {
            return Err(ParseResolverError::TooManySearchDomains);
        }
        search_list.truncate(MAX_SEARCH_DOMAINS);
    }

    resolver.search = Some(search_list);
    Ok(())
}

/// Parses a resolver URL, mirroring Go `createResolver`.
/// Returns `Ok((resolver, skip))`. Link-local availability is assumed
/// (flag stays false); use [`create_resolver_with_local_net`] to inject a check.
pub fn create_resolver(
    resolver_url: &str,
    source: &str,
) -> Result<(Option<Resolver>, bool), ParseResolverError> {
    create_resolver_with_local_net(resolver_url, source, None)
}

/// Like [`create_resolver`], with an injectable replacement for Go
/// `netenv.GetLocalNetwork`: `local_network(ip)` returns whether the IP is in a
/// directly attached network. It is consulted only for the `link-local`
/// parameter; `None` assumes available, so `LinkLocalUnavailable` stays false.
/// With a check, a resolver without IP is unavailable (Go: lookup error).
pub fn create_resolver_with_local_net(
    resolver_url: &str,
    source: &str,
    local_network: Option<&dyn Fn(IpAddr) -> bool>,
) -> Result<(Option<Resolver>, bool), ParseResolverError> {
    let parsed_url = match Url::parse(resolver_url) {
        Ok(u) => u,
        Err(e) => return Err(url_parse_error(resolver_url, e)),
    };

    let scheme = parsed_url.scheme();
    let normalized_scheme = match scheme {
        SERVER_TYPE_DNS | SERVER_TYPE_DOT | SERVER_TYPE_DOH | SERVER_TYPE_TCP => {
            scheme.to_string()
        }
        HTTPS_PROTOCOL => SERVER_TYPE_DOH.to_string(),
        TLS_PROTOCOL => SERVER_TYPE_DOT.to_string(),
        _ => return Err(ParseResolverError::InvalidScheme(scheme.to_string())),
    };

    // Go `url.Values`: the first value of a repeated key wins (`Query().Get`).
    let mut query_map: HashMap<String, String> = HashMap::new();
    let mut query_keys: Vec<String> = Vec::new();
    for (k, v) in parsed_url.query_pairs() {
        if let std::collections::hash_map::Entry::Vacant(e) = query_map.entry(k.into_owned()) {
            query_keys.push(e.key().clone());
            e.insert(v.into_owned());
        }
    }

    // Same order as Go `checkAndSetResolverParamters`: IP/scheme, port, params.
    let (hostname, parsed_ip): (String, Option<IpAddr>) = match parsed_url.host() {
        Some(Host::Ipv4(ip)) => (ip.to_string(), Some(IpAddr::V4(ip))),
        Some(Host::Ipv6(ip)) => (ip.to_string(), Some(IpAddr::V6(ip).to_canonical())),
        Some(Host::Domain(d)) => (
            d.to_string(),
            d.parse::<IpAddr>().ok().map(|ip| ip.to_canonical()),
        ),
        None => (String::new(), None),
    };
    let hostname_is_domain = parsed_ip.is_none();

    if hostname_is_domain
        && normalized_scheme != SERVER_TYPE_DOH
        && normalized_scheme != SERVER_TYPE_DOT
    {
        return Err(ParseResolverError::InvalidIp(hostname));
    }

    let port = default_port(parsed_url.port(), &normalized_scheme)?;

    for key in &query_keys {
        match key.as_str() {
            PARAMETER_NAME
            | PARAMETER_VERIFY
            | PARAMETER_IP
            | PARAMETER_BLOCKED_IF
            | PARAMETER_SEARCH
            | PARAMETER_SEARCH_ONLY
            | PARAMETER_LINK_LOCAL => {}
            _ => return Err(ParseResolverError::UnknownParameter(key.clone())),
        }
    }

    let mut new_resolver = Resolver {
        config_url: resolver_url.to_string(),
        info: ResolverInfo {
            name: query_map.get(PARAMETER_NAME).cloned().unwrap_or_default(),
            r#type: normalized_scheme.clone(),
            source: source.to_string(),
            ip: parsed_ip,
            domain: String::new(),
            ip_scope: IpScope::Global,
            port,
        },
        server_address: String::new(),
        upstream_block_detection: String::new(),
        search: None,
        search_only: false,
        path: raw_path(resolver_url),
        link_local_unavailable: false,
        conn: None,
    };

    let verify_domain = query_map.get(PARAMETER_VERIFY).cloned().unwrap_or_default();
    let param_server_ip = query_map.get(PARAMETER_IP).cloned().unwrap_or_default();

    if normalized_scheme == SERVER_TYPE_DOT || normalized_scheme == SERVER_TYPE_DOH {
        if hostname_is_domain && !verify_domain.is_empty() {
            return Err(ParseResolverError::DomainViaBothHostnameAndVerify);
        }
        if !hostname_is_domain && verify_domain.is_empty() {
            return Err(ParseResolverError::VerifyMustBeSetWhenUsingIpAsDomain);
        }
        if !hostname_is_domain && !param_server_ip.is_empty() {
            return Err(ParseResolverError::IpViaBothHostnameAndIpParam);
        }

        if hostname_is_domain && !param_server_ip.is_empty() {
            new_resolver.info.ip = param_server_ip
                .parse::<IpAddr>()
                .ok()
                .map(|ip| ip.to_canonical());
            new_resolver.server_address = format_host_and_port(&param_server_ip, port);
            new_resolver.info.domain = hostname;
        } else if let Some(ip) = parsed_ip {
            new_resolver.info.domain = verify_domain;
            new_resolver.server_address = format_ip_and_port(ip, port);
        } else {
            new_resolver.info.domain = hostname;
            new_resolver.server_address = format_host_and_port(&new_resolver.info.domain, port);
        }
    } else {
        if !verify_domain.is_empty() {
            return Err(ParseResolverError::DomainVerificationNotSupported);
        }
        if let Some(ip) = parsed_ip {
            new_resolver.server_address = format_ip_and_port(ip, port);
        }
    }

    // Upstream block detection.
    let block_detection = query_map
        .get(PARAMETER_BLOCKED_IF)
        .filter(|v| !v.is_empty())
        .cloned()
        .unwrap_or_else(|| BLOCK_DETECTION_ZERO_IP.to_string());
    match block_detection.as_str() {
        BLOCK_DETECTION_DISABLED
        | BLOCK_DETECTION_EMPTY_ANSWER
        | BLOCK_DETECTION_REFUSED
        | BLOCK_DETECTION_ZERO_IP => {
            new_resolver.upstream_block_detection = block_detection;
        }
        _ => return Err(ParseResolverError::InvalidBlockDetection),
    }

    // IP scope determination.
    if let Some(ip) = new_resolver.info.ip {
        let scope = pm_netstate::ip_scope(ip);
        new_resolver.info.ip_scope = scope;
        if scope.is_localhost() && source == SERVER_SOURCE_OPERATING_SYSTEM {
            return Ok((None, true)); // skip localhost resolvers from the OS
        }
    }

    // Search domains.
    if let Some(searches_str) = query_map.get(PARAMETER_SEARCH) {
        if !searches_str.is_empty() {
            let searches: Vec<String> = searches_str.split(',').map(|s| s.to_string()).collect();
            configure_search_domains(&mut new_resolver, &searches, true)?;
        }
    }

    // Search-only.
    if let Some(value) = query_map.get(PARAMETER_SEARCH_ONLY) {
        new_resolver.search_only = true;
        if !value.is_empty() {
            return Err(ParseResolverError::SearchOnlyNotEmpty);
        }
        if new_resolver.search.as_ref().map_or(0, |s| s.len()) == 0 {
            return Err(ParseResolverError::SearchOnlyWithoutScopes);
        }
    }

    // Link-local.
    if let Some(value) = query_map.get(PARAMETER_LINK_LOCAL) {
        if !value.is_empty() {
            return Err(ParseResolverError::LinkLocalNotEmpty);
        }
        if let Some(check) = local_network {
            new_resolver.link_local_unavailable = match new_resolver.info.ip {
                Some(ip) => !check(ip),
                None => true,
            };
        }
    }

    Ok((Some(new_resolver), false))
}

fn default_port(port: Option<u16>, scheme: &str) -> Result<u16, ParseResolverError> {
    if let Some(port) = port {
        return Ok(port);
    }
    match scheme {
        SERVER_TYPE_DNS | SERVER_TYPE_TCP => Ok(53),
        SERVER_TYPE_DOH => Ok(443),
        SERVER_TYPE_DOT => Ok(853),
        _ => Err(ParseResolverError::CannotDeterminePort(scheme.to_string())),
    }
}

/// Maps a `url` crate error to the message Go `url.Parse` would return.
fn url_parse_error(input: &str, err: url::ParseError) -> ParseResolverError {
    if err == url::ParseError::InvalidPort {
        let port = authority_of(input)
            .rsplit_once(':')
            .map_or("", |(_, p)| p)
            .to_string();
        // Go accepts any digits in url.Parse and fails later in ParseUint.
        if !port.is_empty() && port.bytes().all(|b| b.is_ascii_digit()) {
            return ParseResolverError::InvalidPort(port);
        }
        return ParseResolverError::UrlParse(format!(
            "parse {:?}: invalid port \":{}\" after host",
            input, port
        ));
    }
    ParseResolverError::UrlParse(format!("parse {:?}: {}", input, err))
}

fn authority_of(url_str: &str) -> &str {
    let rest = url_str.find("://").map_or(url_str, |i| &url_str[i + 3..]);
    rest.split(['/', '?', '#']).next().unwrap_or(rest)
}

/// Go `u.Path`: empty when the URL has no path (the `url` crate yields "/"
/// for special schemes such as https), percent-decoded.
fn raw_path(url_str: &str) -> String {
    let rest = url_str.find("://").map_or(url_str, |i| &url_str[i + 3..]);
    let Some(start) = rest.find(['/', '?', '#']) else {
        return String::new();
    };
    let tail = &rest[start..];
    if !tail.starts_with('/') {
        return String::new();
    }
    let end = tail.find(['?', '#']).unwrap_or(tail.len());
    percent_decode(&tail[..end])
}

fn percent_decode(s: &str) -> String {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'%' {
            if let Some(v) = s.get(i + 1..i + 3).and_then(|h| u8::from_str_radix(h, 16).ok()) {
                out.push(v);
                i += 3;
                continue;
            }
        }
        out.push(b[i]);
        i += 1;
    }
    String::from_utf8_lossy(&out).into_owned()
}
