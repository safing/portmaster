//! Parser for Xray/V2Ray share links and subscription payloads.
//!
//! Supported schemes: `vless://`, `vmess://` (both the Base64-JSON and the
//! `uuid@host:port?...` form), `trojan://`, `ss://` (SIP002),
//! `socks://`/`socks5://` and `http://`/`https://`.

use std::collections::hash_map::DefaultHasher;
use std::collections::BTreeMap;
use std::hash::{Hash, Hasher};
use std::net::IpAddr;

use base64::engine::general_purpose::{STANDARD, STANDARD_NO_PAD, URL_SAFE, URL_SAFE_NO_PAD};
use base64::Engine;

use pm_geoip::{country_info, GeoIp};

use crate::model::{Protocol, XrayNode};

/// Errors while parsing a single profile.
#[derive(Debug, Clone, thiserror::Error)]
pub enum ParseError {
    #[error("invalid share link: {0}")]
    Syntax(String),
    #[error("unsupported profile: {0}")]
    Unsupported(String),
    #[error("invalid {0}: {1}")]
    Field(&'static str, String),
}

/// Fields needed both for the node model and for config generation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct ParsedProfile {
    pub protocol: Protocol,
    pub name: String,
    pub address: String,
    pub port: u16,
    /// UUID for VLESS/VMess, password for Trojan, user for SOCKS/HTTP.
    pub credential: String,
    /// Shadowsocks method, when applicable.
    pub method: String,
    /// Shadowsocks/SOCKS/HTTP password, when applicable.
    pub password: String,
    pub network: String,
    pub security: String,
    pub path: String,
    pub host: String,
    pub sni: String,
    pub fingerprint: String,
    pub alpn: Vec<String>,
}

/// Percent-decodes a token (UTF-8 lossy), as share links use query escaping.
pub(crate) fn percent_decode(text: &str) -> String {
    let bytes = text.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'+' => {
                out.push(b' ');
                i += 1;
            }
            b'%' if i + 2 < bytes.len() => {
                if let Ok(byte) = u8::from_str_radix(&text[i + 1..i + 3], 16) {
                    out.push(byte);
                    i += 3;
                    continue;
                }
                out.push(b'%');
                i += 1;
            }
            b => {
                out.push(b);
                i += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

/// Decodes a name/token: percent encoding first, then Base64 when the result
/// is a valid Base64 string that decodes to printable UTF-8 (VMess links often
/// carry Base64 names).
pub(crate) fn decode_text(raw: &str) -> String {
    let percent = percent_decode(raw);
    if percent.len() >= 4
        && percent
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'+' | b'/' | b'-' | b'_' | b'='))
    {
        for engine in [URL_SAFE_NO_PAD, URL_SAFE, STANDARD_NO_PAD, STANDARD] {
            if let Ok(bytes) = engine.decode(percent.as_bytes()) {
                if let Ok(text) = String::from_utf8(bytes) {
                    if !text.is_empty() && !text.chars().any(char::is_control) {
                        return text;
                    }
                }
            }
        }
    }
    percent
}

fn decode_base64(raw: &str) -> Option<Vec<u8>> {
    let raw = raw.trim();
    for engine in [URL_SAFE_NO_PAD, URL_SAFE, STANDARD_NO_PAD, STANDARD] {
        if let Ok(bytes) = engine.decode(raw.as_bytes()) {
            return Some(bytes);
        }
    }
    None
}

fn query_params(query: &str) -> BTreeMap<String, String> {
    let mut params = BTreeMap::new();
    if query.is_empty() {
        return params;
    }
    for item in query.split('&') {
        if item.is_empty() {
            continue;
        }
        let (key, value) = item.split_once('=').unwrap_or((item, ""));
        params.insert(percent_decode(key).to_ascii_lowercase(), percent_decode(value));
    }
    params
}

/// Splits `host:port`, understanding bracketed IPv6 addresses.
fn split_endpoint(authority: &str, default_port: u16) -> Result<(String, u16), ParseError> {
    let authority = authority.trim_end_matches('/');
    let (host, port_text) = if let Some(rest) = authority.strip_prefix('[') {
        let end = rest.find(']').ok_or_else(|| ParseError::Syntax("unclosed IPv6 bracket".into()))?;
        let host = &rest[..end];
        let port = rest[end + 1..].strip_prefix(':').unwrap_or("");
        (host, port)
    } else {
        match authority.rsplit_once(':') {
            Some((host, port)) if !port.contains(':') => (host, port),
            _ => (authority, ""),
        }
    };
    if host.is_empty() {
        return Err(ParseError::Syntax("missing server address".into()));
    }
    let port = if port_text.is_empty() {
        default_port
    } else {
        port_text
            .parse::<u16>()
            .map_err(|error| ParseError::Field("port", error.to_string()))?
    };
    if port == 0 {
        return Err(ParseError::Field("port", "port must not be zero".into()));
    }
    Ok((host.to_string(), port))
}

/// Splits `userinfo@authority`, treating user info as optional.
fn split_userinfo(authority: &str) -> (Option<String>, &str) {
    match authority.split_once('@') {
        Some((user, endpoint)) => (Some(user.to_string()), endpoint),
        None => (None, authority),
    }
}

fn default_profile(protocol: Protocol, name: &str) -> ParsedProfile {
    ParsedProfile {
        protocol,
        name: name.to_string(),
        address: String::new(),
        port: 0,
        credential: String::new(),
        method: String::new(),
        password: String::new(),
        network: "raw".to_string(),
        security: "none".to_string(),
        path: String::new(),
        host: String::new(),
        sni: String::new(),
        fingerprint: String::new(),
        alpn: Vec::new(),
    }
}

fn apply_stream_params(profile: &mut ParsedProfile, params: &BTreeMap<String, String>) {
    if let Some(network) = params.get("type").or_else(|| params.get("network")) {
        profile.network = normalize_network(network);
    }
    if let Some(security) = params.get("security") {
        profile.security = normalize_security(security);
    }
    if let Some(path) = params.get("path") {
        profile.path = path.clone();
    }
    if let Some(host) = params.get("host") {
        profile.host = host.clone();
    }
    if let Some(sni) = params.get("sni").or_else(|| params.get("peer")) {
        profile.sni = sni.clone();
    } else if let Some(host) = params.get("host") {
        profile.sni = host.clone();
    }
    if let Some(fp) = params.get("fp") {
        profile.fingerprint = fp.clone();
    }
    if let Some(alpn) = params.get("alpn") {
        profile.alpn = alpn
            .split(',')
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .map(str::to_string)
            .collect();
    }
}

/// Maps share-link transport names to the names accepted by xray-rs.
pub(crate) fn normalize_network(network: &str) -> String {
    match network.trim().to_ascii_lowercase().as_str() {
        "" | "tcp" | "raw" => "raw".to_string(),
        "websocket" | "ws" => "ws".to_string(),
        "httpupgrade" => "httpupgrade".to_string(),
        "h2" | "xhttp" | "splithttp" => "xhttp".to_string(),
        "grpc" => "grpc".to_string(),
        "kcp" | "mkcp" => "kcp".to_string(),
        "quic" | "hysteria" | "hysteria2" => "hysteria".to_string(),
        other => other.to_string(),
    }
}

fn normalize_security(security: &str) -> String {
    match security.trim().to_ascii_lowercase().as_str() {
        "" | "none" | "auto" => "none".to_string(),
        "tls" | "reality" => security.trim().to_ascii_lowercase(),
        other => other.to_string(),
    }
}

fn parse_vless(body: &str, name: Option<String>) -> Result<ParsedProfile, ParseError> {
    let (authority, query) = body.split_once('?').unwrap_or((body, ""));
    let (id, endpoint) = authority
        .split_once('@')
        .ok_or_else(|| ParseError::Syntax("VLESS link needs uuid@host:port".into()))?;
    let (address, port) = split_endpoint(endpoint, 443)?;
    let mut profile = default_profile(Protocol::Vless, &name.unwrap_or_else(|| "VLESS".into()));
    profile.address = address;
    profile.port = port;
    profile.credential = percent_decode(id);
    let params = query_params(query);
    apply_stream_params(&mut profile, &params);
    Ok(profile)
}

fn parse_vmess(body: &str, fragment: Option<&str>) -> Result<ParsedProfile, ParseError> {
    if body.contains('@') {
        let (authority, query) = body.split_once('?').unwrap_or((body, ""));
        let (id, endpoint) = authority
            .split_once('@')
            .ok_or_else(|| ParseError::Syntax("VMess link needs uuid@host:port".into()))?;
        let (address, port) = split_endpoint(endpoint, 443)?;
        let mut profile = default_profile(Protocol::Vmess, "VMess");
        profile.address = address;
        profile.port = port;
        profile.credential = percent_decode(id);
        let params = query_params(query);
        apply_stream_params(&mut profile, &params);
        if let Some(name) = fragment {
            profile.name = decode_text(name);
        }
        return Ok(profile);
    }

    let decoded = decode_base64(body)
        .ok_or_else(|| ParseError::Syntax("VMess body is not valid Base64".into()))?;
    let json: serde_json::Value = serde_json::from_slice(&decoded)
        .map_err(|error| ParseError::Syntax(format!("VMess JSON: {error}")))?;
    let field = |key: &str| {
        json.get(key)
            .and_then(|value| value.as_str())
            .unwrap_or("")
            .to_string()
    };
    let port = json
        .get("port")
        .and_then(serde_json::Value::as_u64)
        .and_then(|value| u16::try_from(value).ok())
        .ok_or_else(|| ParseError::Field("port", "missing or out of range".into()))?;
    if port == 0 {
        return Err(ParseError::Field("port", "port must not be zero".into()));
    }
    let name = fragment.map(decode_text).unwrap_or_else(|| field("ps"));
    let mut profile = default_profile(Protocol::Vmess, &if name.is_empty() { "VMess".into() } else { name });
    profile.address = field("add");
    profile.port = port;
    profile.credential = field("id");
    if profile.address.is_empty() || profile.credential.is_empty() {
        return Err(ParseError::Syntax("VMess JSON needs add and id".into()));
    }
    profile.network = normalize_network(&field("net"));
    profile.security = normalize_security(&field("tls"));
    profile.path = field("path");
    profile.host = field("host");
    profile.sni = field("sny");
    profile.fingerprint = field("fp");
    let alpn = field("alpn");
    if !alpn.is_empty() {
        profile.alpn = alpn
            .split(',')
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .map(str::to_string)
            .collect();
    }
    Ok(profile)
}

fn parse_trojan(body: &str, name: Option<String>) -> Result<ParsedProfile, ParseError> {
    let (authority, query) = body.split_once('?').unwrap_or((body, ""));
    let (userinfo, endpoint) = split_userinfo(authority);
    let password = userinfo
        .map(|value| percent_decode(&value))
        .ok_or_else(|| ParseError::Syntax("Trojan link needs password@host:port".into()))?;
    let (address, port) = split_endpoint(endpoint, 443)?;
    let mut profile = default_profile(Protocol::Trojan, &name.unwrap_or_else(|| "Trojan".into()));
    profile.address = address;
    profile.port = port;
    profile.password = password;
    let params = query_params(query);
    apply_stream_params(&mut profile, &params);
    // Trojan is always TLS on the wire unless the link explicitly says none.
    if !params.contains_key("security") {
        profile.security = "tls".to_string();
    }
    Ok(profile)
}

fn parse_shadowsocks(body: &str, name: Option<String>) -> Result<ParsedProfile, ParseError> {
    let (authority, query) = body.split_once('?').unwrap_or((body, ""));
    if !query.is_empty() {
        return Err(ParseError::Unsupported("Shadowsocks plugins".into()));
    }
    let (userinfo, endpoint) = split_userinfo(authority);
    let userinfo = userinfo.ok_or_else(|| ParseError::Syntax("SS link needs credentials".into()))?;
    let decoded = if userinfo.contains(':') {
        percent_decode(&userinfo)
    } else {
        let bytes = decode_base64(&userinfo)
            .ok_or_else(|| ParseError::Syntax("invalid SS user info".into()))?;
        String::from_utf8(bytes)
            .map_err(|error| ParseError::Syntax(format!("invalid SS user info: {error}")))?
    };
    let (method, password) = decoded
        .split_once(':')
        .ok_or_else(|| ParseError::Syntax("SS credentials need method:password".into()))?;
    if method.is_empty() || password.is_empty() {
        return Err(ParseError::Syntax("empty SS credentials".into()));
    }
    let (address, port) = split_endpoint(endpoint, 8388)?;
    let mut profile = default_profile(
        Protocol::Shadowsocks,
        &name.unwrap_or_else(|| "Shadowsocks".into()),
    );
    profile.address = address;
    profile.port = port;
    profile.method = method.to_ascii_lowercase();
    profile.password = password.to_string();
    Ok(profile)
}

fn parse_credentials(userinfo: &str) -> (String, String) {
    let decoded = if userinfo.contains(':') {
        percent_decode(userinfo)
    } else if let Some(bytes) = decode_base64(userinfo) {
        String::from_utf8_lossy(&bytes).into_owned()
    } else {
        percent_decode(userinfo)
    };
    decoded
        .split_once(':')
        .map_or((decoded.clone(), String::new()), |(user, pass)| {
            (user.to_string(), pass.to_string())
        })
}

fn parse_socks(body: &str, name: Option<String>) -> Result<ParsedProfile, ParseError> {
    let (authority, query) = body.split_once('?').unwrap_or((body, ""));
    if !query.is_empty() {
        return Err(ParseError::Unsupported("SOCKS link query parameters".into()));
    }
    let (userinfo, endpoint) = split_userinfo(authority);
    let (address, port) = split_endpoint(endpoint, 1080)?;
    let mut profile = default_profile(Protocol::Socks, &name.unwrap_or_else(|| "SOCKS5".into()));
    profile.address = address;
    profile.port = port;
    if let Some(userinfo) = userinfo {
        let (user, pass) = parse_credentials(&userinfo);
        if !user.is_empty() || !pass.is_empty() {
            profile.credential = user;
            profile.password = pass;
        }
    }
    Ok(profile)
}

fn parse_http(body: &str, name: Option<String>, tls: bool) -> Result<ParsedProfile, ParseError> {
    let (authority, query) = body.split_once('?').unwrap_or((body, ""));
    if !query.is_empty() {
        return Err(ParseError::Unsupported("HTTP link query parameters".into()));
    }
    let (userinfo, endpoint) = split_userinfo(authority);
    let (address, port) = split_endpoint(endpoint, if tls { 443 } else { 80 })?;
    let mut profile = default_profile(
        Protocol::Http,
        &name.unwrap_or_else(|| if tls { "HTTPS" } else { "HTTP" }.into()),
    );
    profile.address = address.clone();
    profile.port = port;
    profile.security = if tls { "tls".to_string() } else { "none".to_string() };
    profile.sni = if tls { address } else { String::new() };
    if let Some(userinfo) = userinfo {
        let (user, pass) = parse_credentials(&userinfo);
        if !user.is_empty() {
            profile.credential = user;
            profile.password = pass;
        }
    }
    Ok(profile)
}

/// Parses one share link into its structured form.
pub(crate) fn parse_link(raw: &str) -> Result<ParsedProfile, ParseError> {
    let raw = raw.trim();
    if raw.is_empty() {
        return Err(ParseError::Syntax("empty profile".into()));
    }
    let (scheme, body) = raw
        .split_once("://")
        .ok_or_else(|| ParseError::Syntax("missing :// separator".into()))?;
    let (body, fragment) = match body.split_once('#') {
        Some((body, name)) => (body, Some(name)),
        None => (body, None),
    };
    let name = fragment.map(decode_text);
    match scheme.to_ascii_lowercase().as_str() {
        "vless" => parse_vless(body, name),
        "vmess" => parse_vmess(body, fragment),
        "trojan" => parse_trojan(body, name),
        "ss" | "ssr" => parse_shadowsocks(body, name),
        "socks" | "socks5" | "socks5h" => parse_socks(body, name),
        "http" => parse_http(body, name, false),
        "https" => parse_http(body, name, true),
        other => Err(ParseError::Unsupported(other.to_string())),
    }
}

const NAME_ALIASES: &[(&str, &str)] = &[
    ("россия", "RU"),
    ("russia", "RU"),
    ("сша", "US"),
    ("usa", "US"),
    ("united states", "US"),
    ("great britain", "GB"),
    ("united kingdom", "GB"),
    ("uk", "GB"),
    ("германия", "DE"),
    ("germany", "DE"),
    ("france", "FR"),
    ("франция", "FR"),
    ("netherlands", "NL"),
    ("нидерланды", "NL"),
    ("japan", "JP"),
    ("япония", "JP"),
    ("singapore", "SG"),
    ("сингапур", "SG"),
    ("china", "CN"),
    ("китай", "CN"),
    ("turkey", "TR"),
    ("турция", "TR"),
    ("india", "IN"),
    ("индия", "IN"),
    ("canada", "CA"),
    ("канада", "CA"),
];

fn flag_letter(ch: char) -> Option<char> {
    let code = ch as u32;
    if (0x1F1E6..=0x1F1FF).contains(&code) {
        char::from_u32(code - 0x1F1E6 + b'A' as u32)
    } else {
        None
    }
}

fn code_info(code: &str) -> Option<(String, String)> {
    let info = country_info(code)?;
    Some((code.to_string(), info.name.clone()))
}

/// Guesses the country from a profile name: emoji flags, `[RU]`-style codes,
/// standalone two-letter codes or a (English/Russian) country name.
pub fn country_from_name(name: &str) -> Option<(String, String)> {
    let chars: Vec<char> = name.chars().collect();
    for index in 0..chars.len() {
        if let Some(first) = flag_letter(chars[index]) {
            if let Some(second) = chars.get(index + 1).and_then(|ch| flag_letter(*ch)) {
                let code = format!("{first}{second}");
                if let Some(found) = code_info(&code) {
                    return Some(found);
                }
            }
        }
    }

    let lower = name.to_ascii_lowercase();
    for (alias, code) in NAME_ALIASES {
        if lower.contains(alias) {
            if let Some(found) = code_info(code) {
                return Some(found);
            }
        }
    }

    let tokens: Vec<&str> = name
        .split(|ch: char| !ch.is_ascii_alphanumeric())
        .filter(|token| !token.is_empty())
        .collect();
    for token in tokens {
        if token.len() == 2 && token.bytes().all(|byte| byte.is_ascii_alphabetic()) {
            if let Some(found) = code_info(&token.to_ascii_uppercase()) {
                return Some(found);
            }
        }
    }
    None
}

/// Looks up the country of a node by its IP address using the GeoIP database.
pub fn country_from_address(address: &str, geoip: &GeoIp) -> Option<(String, String)> {
    let ip: IpAddr = address.parse().ok()?;
    let location = geoip.get_location(ip).ok()?;
    if location.country.code.is_empty() {
        return None;
    }
    let name = country_info(&location.country.code)
        .map(|info| info.name.clone())
        .unwrap_or_else(|| location.country.name.clone());
    Some((location.country.code, name))
}

/// Deterministic short ID of a node within a subscription.
pub fn node_id(subscription_id: &str, raw: &str) -> String {
    let mut hasher = DefaultHasher::new();
    subscription_id.hash(&mut hasher);
    raw.hash(&mut hasher);
    format!("{:016x}", hasher.finish())
}

/// Parses one profile into a UI-ready node. The country is taken from the
/// profile name first, then (for literal IP addresses) from `pm_geoip`.
pub fn parse_profile(
    raw: &str,
    subscription_id: &str,
    geoip: Option<&GeoIp>,
) -> Result<XrayNode, ParseError> {
    let parsed = parse_link(raw)?;
    let (country_code, country_name) = country_from_name(&parsed.name)
        .or_else(|| geoip.and_then(|geoip| country_from_address(&parsed.address, geoip)))
        .unwrap_or_default();
    Ok(XrayNode {
        id: node_id(subscription_id, raw),
        subscription_id: subscription_id.to_string(),
        name: parsed.name,
        country_code,
        country_name,
        protocol: parsed.protocol,
        address: parsed.address,
        port: parsed.port,
        raw_config: raw.to_string(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_percent_and_base64_names() {
        assert_eq!(decode_text("Russia%20%231"), "Russia #1");
        assert_eq!(decode_text("REUtRlJBLTAx"), "DE-FRA-01");
    }

    #[test]
    fn detects_country_flags_and_codes() {
        assert_eq!(country_from_name("Node \u{1F1E9}\u{1F1EA} 01"), Some(("DE".into(), country_info("DE").unwrap().name.clone())));
        assert_eq!(country_from_name("[RU] Moscow"), Some(("RU".into(), country_info("RU").unwrap().name.clone())));
        assert_eq!(country_from_name("Frankfurt, Germany #2"), Some(("DE".into(), country_info("DE").unwrap().name.clone())));
    }
}
