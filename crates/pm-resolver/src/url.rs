use std::collections::HashMap;
use std::net::IpAddr;

use url::Url;

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
    if let Ok(ip) = host.parse::<IpAddr>() {
        format_ip_and_port(ip, port)
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
/// Returns `Ok((resolver, skip))`.
pub fn create_resolver(
    resolver_url: &str,
    source: &str,
) -> Result<(Option<Resolver>, bool), ParseResolverError> {
    let parsed_url = match Url::parse(resolver_url) {
        Ok(u) => u,
        Err(url::ParseError::InvalidPort) => {
            let port_str = extract_invalid_port(resolver_url);
            return Err(ParseResolverError::InvalidPort(port_str));
        }
        Err(e) => return Err(ParseResolverError::UrlParse(e.to_string())),
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

    let query_map: HashMap<String, String> = parsed_url.query_pairs().into_owned().collect();
    let raw_query_keys: Vec<String> = parsed_url
        .query()
        .map(|q| {
            q.split('&')
                .filter(|s| !s.is_empty())
                .map(|pair| pair.split('=').next().unwrap_or("").to_string())
                .collect()
        })
        .unwrap_or_default();

    // Check unknown parameters.
    for key in &raw_query_keys {
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

    let hostname = parsed_url.host_str().unwrap_or("");
    let parsed_ip = hostname.parse::<IpAddr>().ok();
    let hostname_is_domain = parsed_ip.is_none();

    if hostname_is_domain
        && normalized_scheme != SERVER_TYPE_DOH
        && normalized_scheme != SERVER_TYPE_DOT
    {
        return Err(ParseResolverError::InvalidIp(hostname.to_string()));
    }

    // Parse port from URL or use scheme default.
    let port = parse_port_from_url(&parsed_url, &normalized_scheme)?;

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
        path: parsed_url.path().to_string(),
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
            new_resolver.info.ip = param_server_ip.parse::<IpAddr>().ok();
            new_resolver.server_address = format_host_and_port(&param_server_ip, port);
            new_resolver.info.domain = hostname.to_string();
        } else if !hostname_is_domain && !verify_domain.is_empty() {
            new_resolver.info.domain = verify_domain;
            new_resolver.server_address = format_host_and_port(hostname, port);
        } else if hostname_is_domain && verify_domain.is_empty() && param_server_ip.is_empty() {
            new_resolver.info.domain = hostname.to_string();
            new_resolver.server_address = format_host_and_port(&new_resolver.info.domain, port);
        }
    } else {
        if !verify_domain.is_empty() {
            return Err(ParseResolverError::DomainVerificationNotSupported);
        }
        new_resolver.server_address = format_host_and_port(hostname, port);
    }

    // Upstream block detection.
    let block_detection = query_map
        .get(PARAMETER_BLOCKED_IF)
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
        let scope = IpScope::from(pm_netstate::ip_scope(ip));
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
    if raw_query_keys.iter().any(|k| k == PARAMETER_SEARCH_ONLY) {
        new_resolver.search_only = true;
        let val = query_map.get(PARAMETER_SEARCH_ONLY).map(|s| s.as_str()).unwrap_or("");
        if !val.is_empty() {
            return Err(ParseResolverError::SearchOnlyNotEmpty);
        }
        if new_resolver.search.as_ref().map_or(0, |s| s.len()) == 0 {
            return Err(ParseResolverError::SearchOnlyWithoutScopes);
        }
    }

    // Link-local.
    if raw_query_keys.iter().any(|k| k == PARAMETER_LINK_LOCAL) {
        let val = query_map.get(PARAMETER_LINK_LOCAL).map(|s| s.as_str()).unwrap_or("");
        if !val.is_empty() {
            return Err(ParseResolverError::LinkLocalNotEmpty);
        }
        new_resolver.link_local_unavailable = true;
    }

    Ok((Some(new_resolver), false))
}

fn parse_port_from_url(url: &Url, scheme: &str) -> Result<u16, ParseResolverError> {
    if let Some(port) = url.port() {
        return Ok(port);
    }

    let host_with_port = url.host_str().unwrap_or("");
    if let Some(idx) = host_with_port.rfind(':') {
        let port_part = &host_with_port[idx + 1..];
        if !port_part.is_empty() && !host_with_port.ends_with(']') {
            return match port_part.parse::<u16>() {
                Ok(p) => Ok(p),
                Err(_) => Err(ParseResolverError::InvalidPort(port_part.to_string())),
            };
        }
    }

    match scheme {
        SERVER_TYPE_DNS | SERVER_TYPE_TCP => Ok(53),
        SERVER_TYPE_DOH => Ok(443),
        SERVER_TYPE_DOT => Ok(853),
        _ => Err(ParseResolverError::CannotDeterminePort(scheme.to_string())),
    }
}

fn extract_invalid_port(url_str: &str) -> String {
    // Find ':' after scheme "://" but before '/' or '?'
    let rest = if let Some(idx) = url_str.find("://") {
        &url_str[idx + 3..]
    } else {
        url_str
    };

    let authority = rest.split(['/', '?']).next().unwrap_or(rest);
    let port_str = if let Some(idx) = authority.rfind(':') {
        &authority[idx + 1..]
    } else {
        authority
    };
    port_str.to_string()
}
