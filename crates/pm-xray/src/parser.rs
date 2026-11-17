//! Parser for Xray/V2Ray share links and subscription payloads.
//!
//! Supported schemes: `vless://`, `vmess://` (both the Base64-JSON and the
//! `uuid@host:port?...` form), `trojan://`, `ss://` (SIP002, with optional
//! plugins), `socks://`/`socks5://`, `http://`/`https://`,
//! `hysteria://`/`hysteria2://`/`hy2://` and `wireguard://` (Base64-encoded
//! wg-quick configuration).
//!
//! Unknown query parameters are deliberately ignored: panels add their own
//! extensions, and a subscription must not break because of one of them.

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

/// Decoded wg-quick configuration, carried from the share link to the
/// outbound generator.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub(crate) struct WireGuardProfile {
    pub secret_key: String,
    pub public_key: String,
    pub pre_shared_key: String,
    /// `host:port` of the first peer, used as the node endpoint.
    pub endpoint: String,
    pub allowed_ips: Vec<String>,
    pub addresses: Vec<String>,
    pub dns: Vec<String>,
    pub mtu: u16,
    pub keep_alive: u16,
}

/// Fields needed both for the node model and for config generation.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(crate) struct ParsedProfile {
    pub protocol: Protocol,
    pub name: String,
    pub address: String,
    pub port: u16,
    /// UUID for VLESS/VMess, password for Trojan, user for SOCKS/HTTP,
    /// authentication token for Hysteria.
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
    /// VLESS encryption (`none` by default).
    pub encryption: String,
    /// XTLS flow on VLESS/Trojan users.
    pub flow: String,
    /// gRPC `serviceName` and RAW `headerType` link parameters.
    pub service_name: String,
    pub header_type: String,
    /// Custom HTTP headers from a `headers={"Name":"value"}` link parameter.
    pub headers: Vec<(String, String)>,
    /// Shadowsocks plugin name and its `plugin-opts` string, when present.
    pub plugin: String,
    pub plugin_opts: String,
    /// Hysteria 2 transport settings.
    pub hy_obfs: String,
    pub hy_obfs_password: String,
    pub hy_pin: String,
    pub hy_mport: String,
    /// Present for `wireguard://` links.
    pub wireguard: Option<WireGuardProfile>,
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
                // Slice the byte buffer: `text` is &str and i is a byte index,
                // so slicing it here can split a multi-byte UTF-8 character.
                let hex = std::str::from_utf8(&bytes[i + 1..i + 3]).ok();
                if let Some(byte) = hex.and_then(|h| u8::from_str_radix(h, 16).ok()) {
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

/// Validates that a proxy server host is a real address and not a placeholder/advertising entry.
pub fn is_valid_proxy_host(host: &str) -> bool {
    let host = host.trim().trim_start_matches('[').trim_end_matches(']');
    if host.is_empty() {
        return false;
    }
    // Reject dummy/placeholder hosts commonly used for ads (e.g. host "1", "0.0.0.0", "127.0.0.1")
    if host == "1" || host == "0.0.0.0" || host == "127.0.0.1" || host.eq_ignore_ascii_case("localhost") {
        return false;
    }
    if let Ok(ip) = host.parse::<IpAddr>() {
        return match ip {
            IpAddr::V4(v4) => !v4.is_unspecified() && !v4.is_loopback() && !v4.is_broadcast(),
            IpAddr::V6(v6) => !v6.is_unspecified() && !v6.is_loopback(),
        };
    }
    // Reject pure numbers without dots (like "1", "123")
    if host.chars().all(|c| c.is_ascii_digit()) {
        return false;
    }
    if host.contains(' ') || host.contains('/') || host.contains('\\') || host.contains('?') || host.contains('#') {
        return false;
    }
    host.chars().all(|c| c.is_ascii_alphanumeric() || c == '.' || c == '-' || c == '_')
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
    if !is_valid_proxy_host(host) {
        return Err(ParseError::Syntax(format!("invalid or dummy proxy host: {host}")));
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
        encryption: String::new(),
        flow: String::new(),
        service_name: String::new(),
        header_type: String::new(),
        headers: Vec::new(),
        plugin: String::new(),
        plugin_opts: String::new(),
        hy_obfs: String::new(),
        hy_obfs_password: String::new(),
        hy_pin: String::new(),
        hy_mport: String::new(),
        wireguard: None,
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
    if let Some(flow) = params.get("flow") {
        profile.flow = flow.clone();
    }
    if let Some(encryption) = params.get("encryption") {
        profile.encryption = encryption.clone();
    }
    if let Some(header_type) = params.get("headertype") {
        profile.header_type = header_type.clone();
    }
    if let Some(service_name) = params
        .get("servicename")
        .or_else(|| params.get("service-name"))
    {
        profile.service_name = service_name.clone();
    }
    if let Some(raw) = params.get("headers") {
        profile.headers = parse_headers(raw);
    }
}

/// Parses a `headers={"Name":"value",...}` link parameter. A malformed value
/// is ignored rather than rejecting the whole profile.
fn parse_headers(raw: &str) -> Vec<(String, String)> {
    let Ok(value) = serde_json::from_str::<serde_json::Value>(raw) else {
        return Vec::new();
    };
    let Some(object) = value.as_object() else {
        return Vec::new();
    };
    object
        .iter()
        .filter_map(|(key, value)| Some((key.clone(), value.as_str()?.to_string())))
        .collect()
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
    if profile.encryption.is_empty() {
        profile.encryption = "none".into();
    }
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
    // v2rayN/panel emit `port` as a string; accept both spellings.
    let port = json
        .get("port")
        .and_then(|value| {
            value.as_u64().or_else(|| value.as_str().and_then(|s| s.parse::<u64>().ok()))
        })
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
    let network = field("net");
    let network = if network.is_empty() { field("type") } else { network };
    profile.network = normalize_network(&network);
    let security = field("tls");
    let security = if security.is_empty() { field("scy") } else { security };
    profile.security = normalize_security(&security);
    profile.path = field("path");
    profile.host = field("host");
    profile.sni = field("sny");
    profile.fingerprint = field("fp");
    profile.flow = field("flow");
    profile.header_type = field("headerType");
    profile.service_name = field("serviceName");
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
    let params = query_params(query);
    if let Some(plugin) = params.get("plugin") {
        profile.plugin = plugin.clone();
    }
    if let Some(opts) = params
        .get("plugin-opts")
        .or_else(|| params.get("pluginopts"))
        .or_else(|| params.get("plugin_opts"))
        .or_else(|| params.get("plugin-options"))
    {
        profile.plugin_opts = opts.clone();
    }
    // Some panel links put all plugin options into a single `plugin` value,
    // e.g. `plugin=obfs-local%3Bobfs%3Dhttp`.
    if profile.plugin_opts.is_empty() {
        let split = profile.plugin.split_once(';').map(|(plugin, opts)| {
            (plugin.to_string(), opts.to_string())
        });
        if let Some((plugin, opts)) = split {
            profile.plugin = plugin;
            profile.plugin_opts = opts;
        }
    }
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

/// Splits a Hysteria endpoint, allowing the port range/port hopping syntax
/// (`host:1000-2000`, `host:1000,2001`) used by Hysteria 2 links. The first
/// port becomes the node port; the full range is kept for config generation.
fn split_hysteria_endpoint(endpoint: &str) -> Result<(String, u16, String), ParseError> {
    let endpoint = endpoint.trim_end_matches('/');
    let (host, port_text) = if let Some(rest) = endpoint.strip_prefix('[') {
        let end = rest.find(']').ok_or_else(|| ParseError::Syntax("unclosed IPv6 bracket".into()))?;
        let host = &rest[..end];
        let port = rest[end + 1..].strip_prefix(':').unwrap_or("");
        (host, port)
    } else {
        match endpoint.rsplit_once(':') {
            Some((host, port)) if !port.contains(':') => (host, port),
            _ => (endpoint, ""),
        }
    };
    if host.is_empty() {
        return Err(ParseError::Syntax("missing server address".into()));
    }
    if port_text.is_empty() {
        return Ok((host.to_string(), 443, String::new()));
    }
    if port_text.contains(['-', ',']) {
        let first = port_text
            .split(['-', ','])
            .next()
            .filter(|value| !value.is_empty())
            .ok_or_else(|| ParseError::Field("port", "empty port range".into()))?;
        let port = first
            .parse::<u16>()
            .map_err(|error| ParseError::Field("port", error.to_string()))?;
        if port == 0 {
            return Err(ParseError::Field("port", "port must not be zero".into()));
        }
        return Ok((host.to_string(), port, port_text.to_string()));
    }
    let port = port_text
        .parse::<u16>()
        .map_err(|error| ParseError::Field("port", error.to_string()))?;
    if port == 0 {
        return Err(ParseError::Field("port", "port must not be zero".into()));
    }
    Ok((host.to_string(), port, String::new()))
}

fn parse_hysteria(body: &str, name: Option<String>) -> Result<ParsedProfile, ParseError> {
    let (authority, query) = body.split_once('?').unwrap_or((body, ""));
    let (auth, endpoint) = match authority.split_once('@') {
        Some((auth, endpoint)) => (percent_decode(auth), endpoint),
        None => (String::new(), authority),
    };
    let (address, port, port_range) = split_hysteria_endpoint(endpoint)?;
    let mut profile = default_profile(
        Protocol::Hysteria2,
        &name.unwrap_or_else(|| "Hysteria 2".into()),
    );
    profile.address = address.clone();
    profile.port = port;
    profile.network = "hysteria".into();
    profile.security = "tls".into();
    profile.sni = address;
    profile.alpn = vec!["h3".into()];
    profile.hy_mport = port_range;
    profile.credential = auth;

    let params = query_params(query);
    if let Some(sni) = params.get("sni") {
        profile.sni = sni.clone();
    }
    if let Some(fingerprint) = params.get("fp") {
        profile.fingerprint = fingerprint.clone();
    }
    if let Some(pin) = params.get("pinsha256").or_else(|| params.get("pin")) {
        profile.hy_pin = pin.replace([':', '-'], "");
    }
    // `insecure`/`allowInsecure` are intentionally ignored: both xray-rs and
    // Xray-core refuse unverified TLS, and a pin replaces the CA check.
    if let Some(obfs) = params.get("obfs") {
        profile.hy_obfs = obfs.clone();
    }
    if let Some(password) = params
        .get("obfs-password")
        .or_else(|| params.get("obfspassword"))
    {
        profile.hy_obfs_password = password.clone();
    }
    if let Some(mport) = params.get("mport") {
        profile.hy_mport = mport.clone();
    }
    // Hysteria 1 links carry the token in the query instead of the userinfo.
    if profile.credential.is_empty() {
        if let Some(auth) = params.get("auth") {
            profile.credential = auth.clone();
        }
    }
    if profile.credential.is_empty() {
        return Err(ParseError::Syntax("Hysteria link needs authentication".into()));
    }
    Ok(profile)
}

type WgFields = BTreeMap<String, Vec<String>>;

/// Parses the key/value sections of a wg-quick configuration. Unknown keys
/// are ignored so a future wg-quick option cannot break a subscription.
fn wg_fields(text: &str) -> Result<(WgFields, Vec<WgFields>), ParseError> {
    let mut interface = WgFields::new();
    let mut peers = Vec::new();
    let mut in_interface = false;
    for raw in text.lines() {
        let line = raw.split('#').next().unwrap_or("").trim();
        if line.is_empty() {
            continue;
        }
        if line.eq_ignore_ascii_case("[Interface]") {
            if in_interface || !peers.is_empty() {
                return Err(ParseError::Syntax("duplicate [Interface] section".into()));
            }
            in_interface = true;
            continue;
        }
        if line.eq_ignore_ascii_case("[Peer]") {
            if !in_interface {
                return Err(ParseError::Syntax("[Peer] without [Interface]".into()));
            }
            peers.push(WgFields::new());
            continue;
        }
        if !in_interface {
            return Err(ParseError::Syntax("field outside a WireGuard section".into()));
        }
        let (key, value) = line
            .split_once('=')
            .ok_or_else(|| ParseError::Syntax("WireGuard field needs '='".into()))?;
        let fields = peers.last_mut().unwrap_or(&mut interface);
        fields
            .entry(key.trim().to_ascii_lowercase())
            .or_default()
            .push(value.trim().to_string());
    }
    if !in_interface || peers.is_empty() {
        return Err(ParseError::Syntax("WireGuard needs [Interface] and [Peer]".into()));
    }
    Ok((interface, peers))
}

fn wg_one(fields: &WgFields, key: &str) -> String {
    fields
        .get(key)
        .and_then(|values| values.first())
        .cloned()
        .unwrap_or_default()
}

fn wg_list(fields: &WgFields, key: &str) -> Vec<String> {
    fields
        .get(key)
        .map(|values| {
            values
                .iter()
                .flat_map(|value| value.split(','))
                .map(str::trim)
                .filter(|value| !value.is_empty())
                .map(str::to_string)
                .collect()
        })
        .unwrap_or_default()
}

fn parse_wireguard(body: &str, name: Option<String>) -> Result<ParsedProfile, ParseError> {
    // Panels encode the whole wg-quick file as Base64 in the link body; accept
    // a percent-encoded plain file as a fallback.
    let decoded = decode_base64(body).unwrap_or_else(|| percent_decode(body).into_bytes());
    let text = String::from_utf8_lossy(&decoded);
    let (interface, peers) = wg_fields(&text)?;
    let secret_key = wg_one(&interface, "privatekey");
    if secret_key.is_empty() {
        return Err(ParseError::Field("privatekey", "missing".into()));
    }
    let peer = &peers[0];
    let public_key = wg_one(peer, "publickey");
    let endpoint = wg_one(peer, "endpoint");
    if public_key.is_empty() || endpoint.is_empty() {
        return Err(ParseError::Syntax("WireGuard peer needs PublicKey and Endpoint".into()));
    }
    let (address, port) = split_endpoint(&endpoint, 0)?;
    let mtu = wg_one(&interface, "mtu")
        .parse::<u16>()
        .unwrap_or(1420);
    let keep_alive = match wg_one(peer, "persistentkeepalive").to_ascii_lowercase().as_str() {
        "off" => 0,
        value => value.parse::<u16>().unwrap_or(0),
    };
    let mut profile = default_profile(
        Protocol::Wireguard,
        &name.unwrap_or_else(|| "WireGuard".into()),
    );
    profile.address = address;
    profile.port = port;
    profile.wireguard = Some(WireGuardProfile {
        secret_key,
        public_key,
        pre_shared_key: wg_one(peer, "presharedkey"),
        endpoint,
        allowed_ips: wg_list(peer, "allowedips"),
        addresses: wg_list(&interface, "address"),
        dns: wg_list(&interface, "dns"),
        mtu,
        keep_alive,
    });
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
        "ss" => parse_shadowsocks(body, name),
        "ssr" => Err(ParseError::Unsupported("ShadowsocksR is not supported".into())),
        "socks" | "socks5" | "socks5h" => parse_socks(body, name),
        "http" => parse_http(body, name, false),
        "https" => parse_http(body, name, true),
        "hysteria" | "hysteria2" | "hy2" => parse_hysteria(body, name),
        "wireguard" | "wg" => parse_wireguard(body, name),
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
    if code == "EU" {
        return Some(("EU".to_string(), "European Union".to_string()));
    }
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

fn resolve_ip(address: &str) -> Option<IpAddr> {
    if let Ok(ip) = address.parse::<IpAddr>() {
        return Some(ip);
    }
    use std::net::ToSocketAddrs;
    if let Ok(mut addrs) = (address, 0).to_socket_addrs() {
        return addrs.next().map(|s| s.ip());
    }
    None
}

/// Looks up the country of a node by its IP address or hostname using the GeoIP database.
pub fn country_from_address(address: &str, geoip: &GeoIp) -> Option<(String, String)> {
    let ip = resolve_ip(address)?;
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
/// profile name first, then from `pm_geoip` (resolving DNS if necessary).
/// Coordinates (latitude/longitude) are populated so nodes render accurately on the world map.
pub fn parse_profile(
    raw: &str,
    subscription_id: &str,
    geoip: Option<&GeoIp>,
) -> Result<XrayNode, ParseError> {
    let parsed = parse_link(raw)?;
    let (mut country_code, mut country_name) = country_from_name(&parsed.name).unwrap_or_default();
    let mut latitude = None;
    let mut longitude = None;

    if let Some(geoip) = geoip {
        if let Some(ip) = resolve_ip(&parsed.address) {
            if let Ok(location) = geoip.get_location(ip) {
                if country_code.is_empty() && !location.country.code.is_empty() {
                    country_code = location.country.code.clone();
                    country_name = country_info(&location.country.code)
                        .map(|info| info.name.clone())
                        .unwrap_or_else(|| location.country.name.clone());
                }
                if location.coordinates.latitude != 0.0 || location.coordinates.longitude != 0.0 {
                    latitude = Some(location.coordinates.latitude);
                    longitude = Some(location.coordinates.longitude);
                }
            }
        }
    }

    if latitude.is_none() && !country_code.is_empty() {
        if country_code == "EU" {
            latitude = Some(50.85);
            longitude = Some(4.35);
        } else if let Some(info) = country_info(&country_code) {
            if info.center.latitude != 0.0 || info.center.longitude != 0.0 {
                latitude = Some(info.center.latitude);
                longitude = Some(info.center.longitude);
            }
        }
    }

    Ok(XrayNode {
        id: node_id(subscription_id, raw),
        subscription_id: subscription_id.to_string(),
        name: parsed.name,
        country_code,
        country_name,
        latitude,
        longitude,
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
