//! Subscription storage, HTTP fetching and payload decoding.

use std::collections::hash_map::DefaultHasher;
use std::collections::HashSet;
use std::fmt::Write;
use std::hash::{Hash, Hasher};
use std::io::Read;
use std::sync::Arc;

use base64::engine::general_purpose::{STANDARD, STANDARD_NO_PAD, URL_SAFE, URL_SAFE_NO_PAD};
use base64::Engine;
use pm_database::{DatabaseInfo, Databases, Interface, Key, Meta, Options, Query, Record};
use pm_geoip::GeoIp;
use sha2::{Digest, Sha256};

use crate::model::{AddSubscriptionRequest, Subscription, UpdateSubscriptionRequest, XrayNode};
use crate::parser::{node_id, parse_profile, percent_decode};
use crate::{Error, Result};

/// Name of the database holding subscriptions, parsed nodes and the active node.
pub const XRAY_DB: &str = "xray";
const SUBSCRIPTION_PREFIX: &str = "subscription:";
const NODE_PREFIX: &str = "node:";
const ACTIVE_KEY: &str = "active";

const USER_AGENTS: &[&str] = &[
    "v2rayN/6.40",
    "sing-box/1.9.0",
    "ClashMeta/1.18.0",
    "NekoBox/1.3.0",
    "Portmaster Xray",
];

const KNOWN_SCHEMES: &[&str] = &[
    "vless://",
    "hysteria2://",
    "hysteria://",
    "hy2://",
    "trojan://",
    "ss://",
    "ssr://",
    "vmess://",
    "wireguard://",
    "wg://",
    "socks5://",
    "socks://",
    "tuic://",
    "https://",
    "http://",
];

fn deterministic_hwid(url: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(b"hwid_salt:");
    hasher.update(url.as_bytes());
    let result = hasher.finalize();
    hex::encode(&result[..16])
}

fn url_encode(input: &str) -> String {
    let mut encoded = String::with_capacity(input.len());
    for b in input.bytes() {
        match b {
            b'a'..=b'z' | b'A'..=b'Z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                encoded.push(b as char);
            }
            _ => {
                let _ = write!(encoded, "%{:02X}", b);
            }
        }
    }
    encoded
}

fn id_for(url: &str) -> String {
    let mut hasher = DefaultHasher::new();
    url.hash(&mut hasher);
    format!("{:016x}", hasher.finish())
}

/// Database-local key of a subscription record (without the `db:` prefix).
fn subscription_key(id: &str) -> String {
    format!("{SUBSCRIPTION_PREFIX}{id}")
}

/// Database-local key of a node record.
fn node_key(subscription_id: &str, node_id: &str) -> String {
    format!("{NODE_PREFIX}{subscription_id}:{node_id}")
}

fn full_key(local: &str) -> String {
    format!("{XRAY_DB}:{local}")
}

/// Abstraction over HTTP fetching, so refresh can be tested offline.
pub trait SubscriptionFetcher: Send + Sync {
    fn fetch(&self, url: &str) -> std::result::Result<String, String>;
}

/// Blocking HTTPS client based on `ureq`, the client already used by the
/// update module. API handlers are synchronous, so a blocking client avoids
/// nesting a Tokio runtime inside the core server.
pub struct HttpFetcher {
    agent: ureq::Agent,
}

impl Default for HttpFetcher {
    fn default() -> Self {
        Self::new()
    }
}

impl HttpFetcher {
    pub fn new() -> Self {
        HttpFetcher {
            agent: ureq::Agent::new_with_defaults(),
        }
    }
}

/// Extracts per-subscription request settings from the URL.
fn subscription_request(url: &str) -> (String, Vec<(String, String)>) {
    let mut headers = Vec::new();
    let (mut url, basic) = match url.split_once("://") {
        Some((scheme, rest)) => match rest.split_once('@') {
            Some((userinfo, endpoint))
                if !userinfo.contains(['/', '?', '#'])
                    && !endpoint.is_empty()
                    && userinfo.contains(':') =>
            {
                let credential = STANDARD.encode(percent_decode(userinfo).as_bytes());
                (
                    format!("{scheme}://{endpoint}"),
                    Some(("Authorization".to_string(), format!("Basic {credential}"))),
                )
            }
            _ => (url.to_string(), None),
        },
        None => (url.to_string(), None),
    };
    if let Some(header) = basic {
        headers.push(header);
    }
    if let Some((base, query)) = url.split_once('?') {
        let mut kept = Vec::new();
        for item in query.split('&') {
            if let Some((name, value)) = item.split_once('=') {
                if let Some(header) = name.strip_prefix("header-") {
                    if !header.is_empty() {
                        headers.push((percent_decode(header), percent_decode(value)));
                        continue;
                    }
                }
            }
            kept.push(item);
        }
        url = if kept.is_empty() {
            base.to_string()
        } else {
            format!("{base}?{}", kept.join("&"))
        };
    }
    (url, headers)
}

impl SubscriptionFetcher for HttpFetcher {
    fn fetch(&self, url: &str) -> std::result::Result<String, String> {
        let trimmed = url.trim();
        // If not an HTTP/HTTPS URL, it's a direct share link or local payload
        if !trimmed.starts_with("http://") && !trimmed.starts_with("https://") {
            return Ok(trimmed.to_string());
        }

        let (target_url, headers) = subscription_request(trimmed);
        let hwid = deterministic_hwid(&target_url);

        let mut last_error = String::new();
        for ua in USER_AGENTS {
            let mut request = self
                .agent
                .get(&target_url)
                .header("User-Agent", *ua)
                .header("Accept", "*/*")
                .header("Accept-Language", "ru,en;q=0.9")
                .header("x-hwid", &hwid)
                .header("x-device-os", "Windows");

            for (name, value) in &headers {
                request = request.header(name, value);
            }

            match request.call() {
                Ok(mut response) => {
                    let mut data = Vec::new();
                    if let Err(err) = response.body_mut().as_reader().read_to_end(&mut data) {
                        last_error = format!("failed to read body: {err}");
                        continue;
                    }
                    if data.len() > 16 * 1024 * 1024 {
                        return Err("subscription is too large (over 16 MiB)".to_string());
                    }
                    return Ok(String::from_utf8_lossy(&data).into_owned());
                }
                Err(error) => {
                    last_error = format!("request failed with UA {ua}: {error}");
                }
            }
        }
        Err(last_error)
    }
}

fn extract_direct_links(text: &str, out: &mut Vec<String>, seen: &mut HashSet<String>) {
    let bytes = text.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        let mut matched_scheme = None;
        for scheme in KNOWN_SCHEMES {
            if text[i..].starts_with(scheme) {
                matched_scheme = Some(*scheme);
                break;
            }
        }

        if let Some(scheme) = matched_scheme {
            let start = i;
            let mut end = start + scheme.len();
            while end < bytes.len() {
                let ch = bytes[end];
                if ch.is_ascii_whitespace()
                    || ch == b'"'
                    || ch == b'\''
                    || ch == b'`'
                    || ch == b'<'
                    || ch == b'>'
                    || ch == b'{'
                    || ch == b'}'
                    || ch == b'\\'
                {
                    break;
                }
                end += 1;
            }

            let mut link = &text[start..end];
            while link.ends_with(',') || link.ends_with(';') || link.ends_with('.') || link.ends_with(')') {
                link = &link[..link.len() - 1];
            }

            if link.len() > scheme.len() && !seen.contains(link) {
                seen.insert(link.to_string());
                out.push(link.to_string());
            }
            i = end;
        } else {
            i += 1;
        }
    }
}

fn extract_from_json(text: &str, out: &mut Vec<String>, seen: &mut HashSet<String>) {
    let Ok(val) = serde_json::from_str::<serde_json::Value>(text) else {
        return;
    };
    parse_json_value(&val, out, seen);
}

fn parse_json_value(val: &serde_json::Value, out: &mut Vec<String>, seen: &mut HashSet<String>) {
    if let Some(arr) = val.as_array() {
        for item in arr {
            parse_json_value(item, out, seen);
        }
        return;
    }

    let Some(obj) = val.as_object() else {
        return;
    };

    let remarks = obj.get("remarks").and_then(|v| v.as_str()).unwrap_or("");

    if let Some(outbounds) = obj.get("outbounds").and_then(|v| v.as_array()) {
        for ob in outbounds {
            parse_outbound_object(ob, remarks, out, seen);
        }
    }
    if let Some(outbound) = obj.get("outbound") {
        parse_outbound_object(outbound, remarks, out, seen);
    }
}

fn parse_outbound_object(
    ob: &serde_json::Value,
    parent_remarks: &str,
    out: &mut Vec<String>,
    seen: &mut HashSet<String>,
) {
    let Some(ob_map) = ob.as_object() else { return; };
    let tag = ob_map
        .get("tag")
        .and_then(|v| v.as_str())
        .filter(|s| !s.is_empty())
        .unwrap_or(parent_remarks);

    // 1. Sing-box outbound (field `type`)
    if let Some(ob_type) = ob_map.get("type").and_then(|v| v.as_str()) {
        let server = ob_map.get("server").and_then(|v| v.as_str()).unwrap_or_default();
        let port = ob_map.get("server_port").and_then(|v| v.as_u64()).unwrap_or(443);
        if server.is_empty() || server == "0.0.0.0" {
            return;
        }
        let final_tag = if tag.is_empty() { server } else { tag };

        match ob_type {
            "vless" => {
                let uuid = ob_map.get("uuid").and_then(|v| v.as_str()).unwrap_or_default();
                if uuid.is_empty() || uuid == "00000000-0000-0000-0000-000000000000" {
                    return;
                }
                let mut params = Vec::new();
                params.push("encryption=none".to_string());
                if let Some(flow) = ob_map.get("flow").and_then(|v| v.as_str()) {
                    if !flow.is_empty() {
                        params.push(format!("flow={flow}"));
                    }
                }
                let transport = ob_map.get("transport");
                let net_type = transport
                    .and_then(|t| t.get("type"))
                    .or_else(|| ob_map.get("network"))
                    .and_then(|v| v.as_str())
                    .unwrap_or("tcp");
                params.push(format!("type={net_type}"));
                if let Some(path) = transport.and_then(|t| t.get("path")).and_then(|v| v.as_str()) {
                    params.push(format!("path={}", url_encode(path)));
                }
                if let Some(host) = transport
                    .and_then(|t| t.get("host").or_else(|| t.get("headers").and_then(|h| h.get("Host"))))
                    .and_then(|v| v.as_str())
                {
                    params.push(format!("host={host}"));
                }

                let tls = ob_map.get("tls");
                if tls.and_then(|t| t.get("enabled")).and_then(|v| v.as_bool()).unwrap_or(false) {
                    let reality = tls.and_then(|t| t.get("reality"));
                    if reality.and_then(|r| r.get("enabled")).and_then(|v| v.as_bool()).unwrap_or(false) {
                        params.push("security=reality".to_string());
                        if let Some(pbk) = reality.and_then(|r| r.get("public_key")).and_then(|v| v.as_str()) {
                            params.push(format!("pbk={pbk}"));
                        }
                        if let Some(sid) = reality.and_then(|r| r.get("short_id")).and_then(|v| v.as_str()) {
                            params.push(format!("sid={sid}"));
                        }
                    } else {
                        params.push("security=tls".to_string());
                    }
                    if let Some(sni) = tls.and_then(|t| t.get("server_name")).and_then(|v| v.as_str()) {
                        params.push(format!("sni={sni}"));
                    }
                    if let Some(fp) = tls.and_then(|t| t.get("utls")).and_then(|u| u.get("fingerprint")).and_then(|v| v.as_str()) {
                        params.push(format!("fp={fp}"));
                    }
                } else {
                    params.push("security=none".to_string());
                }

                let query = params.join("&");
                let link = format!("vless://{uuid}@{server}:{port}?{query}#{}", url_encode(final_tag));
                if !seen.contains(&link) {
                    seen.insert(link.clone());
                    out.push(link);
                }
            }
            "hysteria2" | "hysteria" => {
                let auth = ob_map
                    .get("password")
                    .or_else(|| ob_map.get("auth_str"))
                    .and_then(|v| v.as_str())
                    .unwrap_or_default();
                let tls = ob_map.get("tls");
                let sni = tls.and_then(|t| t.get("server_name")).and_then(|v| v.as_str()).unwrap_or(server);
                let mut q = vec![format!("sni={sni}")];
                if tls.and_then(|t| t.get("insecure")).and_then(|v| v.as_bool()).unwrap_or(false) {
                    q.push("insecure=1".to_string());
                }
                let auth_prefix = if auth.is_empty() { String::new() } else { format!("{auth}@") };
                let link = format!(
                    "hysteria2://{auth_prefix}{server}:{port}/?{}#{}",
                    q.join("&"),
                    url_encode(final_tag)
                );
                if !seen.contains(&link) {
                    seen.insert(link.clone());
                    out.push(link);
                }
            }
            "trojan" => {
                let password = ob_map.get("password").and_then(|v| v.as_str()).unwrap_or_default();
                let sni = ob_map
                    .get("tls")
                    .and_then(|t| t.get("server_name"))
                    .and_then(|v| v.as_str())
                    .unwrap_or(server);
                let link = format!(
                    "trojan://{password}@{server}:{port}?security=tls&sni={sni}#{}",
                    url_encode(final_tag)
                );
                if !seen.contains(&link) {
                    seen.insert(link.clone());
                    out.push(link);
                }
            }
            "shadowsocks" => {
                let method = ob_map.get("method").and_then(|v| v.as_str()).unwrap_or("aes-256-gcm");
                let password = ob_map.get("password").and_then(|v| v.as_str()).unwrap_or_default();
                let cred = STANDARD.encode(format!("{method}:{password}"));
                let link = format!("ss://{cred}@{server}:{port}#{}", url_encode(final_tag));
                if !seen.contains(&link) {
                    seen.insert(link.clone());
                    out.push(link);
                }
            }
            _ => {}
        }
        return;
    }

    // 2. Xray / V2Ray outbound (field `protocol`)
    if let Some(proto) = ob_map.get("protocol").and_then(|v| v.as_str()) {
        let settings = ob_map.get("settings");
        let ss = ob_map.get("streamSettings");

        match proto {
            "vless" => {
                let vnext = settings.and_then(|s| s.get("vnext")).and_then(|v| v.as_array());
                if let Some(vnext) = vnext {
                    for vn in vnext {
                        let address = vn.get("address").and_then(|v| v.as_str()).unwrap_or_default();
                        let port = vn.get("port").and_then(|v| v.as_u64()).unwrap_or(443);
                        if address.is_empty() || address == "0.0.0.0" {
                            continue;
                        }
                        let final_tag = if tag.is_empty() { address } else { tag };
                        if let Some(users) = vn.get("users").and_then(|u| u.as_array()) {
                            for u in users {
                                let uuid = u.get("id").and_then(|v| v.as_str()).unwrap_or_default();
                                if uuid.is_empty() || uuid == "00000000-0000-0000-0000-000000000000" {
                                    continue;
                                }
                                let encryption = u.get("encryption").and_then(|v| v.as_str()).unwrap_or("none");
                                let network = ss.and_then(|s| s.get("network")).and_then(|v| v.as_str()).unwrap_or("tcp");
                                let security = ss.and_then(|s| s.get("security")).and_then(|v| v.as_str()).unwrap_or("none");

                                let mut params = Vec::new();
                                params.push(format!("encryption={encryption}"));
                                if let Some(flow) = u.get("flow").and_then(|v| v.as_str()) {
                                    if !flow.is_empty() {
                                        params.push(format!("flow={flow}"));
                                    }
                                }
                                params.push(format!("type={network}"));
                                params.push(format!("security={security}"));

                                if security == "reality" {
                                    if let Some(rs) = ss.and_then(|s| s.get("realitySettings")) {
                                        if let Some(sni) = rs.get("serverName").and_then(|v| v.as_str()) {
                                            params.push(format!("sni={sni}"));
                                        }
                                        if let Some(pbk) = rs.get("publicKey").and_then(|v| v.as_str()) {
                                            params.push(format!("pbk={pbk}"));
                                        }
                                        if let Some(sid) = rs.get("shortId").and_then(|v| v.as_str()) {
                                            params.push(format!("sid={sid}"));
                                        }
                                        if let Some(fp) = rs.get("fingerprint").and_then(|v| v.as_str()) {
                                            params.push(format!("fp={fp}"));
                                        }
                                    }
                                } else if security == "tls" {
                                    if let Some(ts) = ss.and_then(|s| s.get("tlsSettings")) {
                                        if let Some(sni) = ts.get("serverName").and_then(|v| v.as_str()) {
                                            params.push(format!("sni={sni}"));
                                        }
                                        if let Some(fp) = ts.get("fingerprint").and_then(|v| v.as_str()) {
                                            params.push(format!("fp={fp}"));
                                        }
                                    }
                                }

                                if network == "ws" {
                                    if let Some(ws) = ss.and_then(|s| s.get("wsSettings")) {
                                        if let Some(path) = ws.get("path").and_then(|v| v.as_str()) {
                                            params.push(format!("path={}", url_encode(path)));
                                        }
                                        if let Some(host) = ws.get("headers").and_then(|h| h.get("Host")).and_then(|v| v.as_str()) {
                                            params.push(format!("host={host}"));
                                        }
                                    }
                                } else if network == "grpc" {
                                    if let Some(gs) = ss.and_then(|s| s.get("grpcSettings")) {
                                        if let Some(sn) = gs.get("serviceName").and_then(|v| v.as_str()) {
                                            params.push(format!("serviceName={sn}"));
                                        }
                                    }
                                }

                                let query = params.join("&");
                                let link = format!("vless://{uuid}@{address}:{port}?{query}#{}", url_encode(final_tag));
                                if !seen.contains(&link) {
                                    seen.insert(link.clone());
                                    out.push(link);
                                }
                            }
                        }
                    }
                }
            }
            "hysteria" | "hysteria2" => {
                let address = settings.and_then(|s| s.get("address")).and_then(|v| v.as_str()).unwrap_or_default();
                let port = settings.and_then(|s| s.get("port")).and_then(|v| v.as_u64()).unwrap_or(443);
                if address.is_empty() || address == "0.0.0.0" {
                    return;
                }
                let auth = settings.and_then(|s| s.get("auth")).and_then(|v| v.as_str()).unwrap_or_default();
                let sni = ss
                    .and_then(|s| s.get("tlsSettings"))
                    .and_then(|t| t.get("serverName"))
                    .and_then(|v| v.as_str())
                    .unwrap_or(address);
                let final_tag = if tag.is_empty() { address } else { tag };
                let auth_prefix = if auth.is_empty() { String::new() } else { format!("{auth}@") };
                let link = format!("hysteria2://{auth_prefix}{address}:{port}/?sni={sni}#{}", url_encode(final_tag));
                if !seen.contains(&link) {
                    seen.insert(link.clone());
                    out.push(link);
                }
            }
            "trojan" => {
                if let Some(servers) = settings.and_then(|s| s.get("servers")).and_then(|v| v.as_array()) {
                    for s in servers {
                        let address = s.get("address").and_then(|v| v.as_str()).unwrap_or_default();
                        let port = s.get("port").and_then(|v| v.as_u64()).unwrap_or(443);
                        let password = s.get("password").and_then(|v| v.as_str()).unwrap_or_default();
                        if address.is_empty() { continue; }
                        let final_tag = if tag.is_empty() { address } else { tag };
                        let sni = ss.and_then(|st| st.get("tlsSettings")).and_then(|t| t.get("serverName")).and_then(|v| v.as_str()).unwrap_or(address);
                        let link = format!("trojan://{password}@{address}:{port}?security=tls&sni={sni}#{}", url_encode(final_tag));
                        if !seen.contains(&link) {
                            seen.insert(link.clone());
                            out.push(link);
                        }
                    }
                }
            }
            "shadowsocks" => {
                if let Some(servers) = settings.and_then(|s| s.get("servers")).and_then(|v| v.as_array()) {
                    for s in servers {
                        let address = s.get("address").and_then(|v| v.as_str()).unwrap_or_default();
                        let port = s.get("port").and_then(|v| v.as_u64()).unwrap_or(8388);
                        let method = s.get("method").and_then(|v| v.as_str()).unwrap_or("aes-256-gcm");
                        let password = s.get("password").and_then(|v| v.as_str()).unwrap_or_default();
                        if address.is_empty() { continue; }
                        let final_tag = if tag.is_empty() { address } else { tag };
                        let cred = STANDARD.encode(format!("{method}:{password}"));
                        let link = format!("ss://{cred}@{address}:{port}#{}", url_encode(final_tag));
                        if !seen.contains(&link) {
                            seen.insert(link.clone());
                            out.push(link);
                        }
                    }
                }
            }
            _ => {}
        }
    }
}

fn try_decode_base64_string(input: &str) -> Option<String> {
    let compact: String = input.chars().filter(|ch| !ch.is_whitespace()).collect();
    if compact.len() < 4 {
        return None;
    }
    let mut padded = compact.clone();
    while !padded.len().is_multiple_of(4) {
        padded.push('=');
    }
    for candidate in [&compact, &padded] {
        for engine in [URL_SAFE_NO_PAD, URL_SAFE, STANDARD_NO_PAD, STANDARD] {
            if let Ok(bytes) = engine.decode(candidate.as_bytes()) {
                if let Ok(text) = String::from_utf8(bytes) {
                    if text.contains("://") || text.contains("outbounds") || text.contains("[Interface]") {
                        return Some(text);
                    }
                }
            }
        }
    }
    None
}

/// Decodes a subscription body into individual share links. Accepts a plain
/// newline-separated list, Base64 (standard or URL-safe, with or without
/// padding), Sing-Box JSON outbounds, and Xray/V2Ray JSON configs.
pub fn decode_payload(payload: &str) -> std::result::Result<Vec<String>, String> {
    let trimmed = payload.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }

    if trimmed.contains("[Interface]") && trimmed.contains("[Peer]") {
        return Ok(vec![format!(
            "wireguard://{}",
            URL_SAFE_NO_PAD.encode(trimmed.as_bytes())
        )]);
    }

    let mut out = Vec::new();
    let mut seen = HashSet::new();

    let parse_candidate = |text: &str, out: &mut Vec<String>, seen: &mut HashSet<String>| {
        let t = text.trim();
        if t.starts_with('{') || t.starts_with('[') {
            extract_from_json(t, out, seen);
            if !out.is_empty() {
                return;
            }
        }
        extract_direct_links(t, out, seen);
        if out.is_empty() {
            extract_from_json(t, out, seen);
        }
    };

    // 1. Direct text scan / JSON parse
    parse_candidate(trimmed, &mut out, &mut seen);

    // 2. Base64 decode
    if out.is_empty() {
        if let Some(decoded) = try_decode_base64_string(trimmed) {
            parse_candidate(&decoded, &mut out, &mut seen);
            // Double base64 check
            if out.is_empty() {
                if let Some(double_decoded) = try_decode_base64_string(&decoded) {
                    parse_candidate(&double_decoded, &mut out, &mut seen);
                }
            }
        }
    }

    // 4. Line-by-line fallback
    if out.is_empty() {
        for line in trimmed.lines() {
            let l = line.trim();
            if l.contains("://") && !seen.contains(l) {
                seen.insert(l.to_string());
                out.push(l.to_string());
            }
        }
    }

    if out.is_empty() {
        return Err("payload contains no valid proxy profiles (checked VLESS, Hysteria2, Trojan, Shadowsocks, WireGuard, Sing-Box & Xray formats)".to_string());
    }

    Ok(out)
}

/// CRUD and node storage on top of the `xray` database.
pub struct SubscriptionStore {
    db: Interface,
}

impl SubscriptionStore {
    /// Creates the store, registering the `xray` database if needed.
    pub fn new(databases: &Arc<Databases>) -> Result<Self> {
        if !databases.is_registered(XRAY_DB) {
            databases.register(DatabaseInfo {
                name: XRAY_DB.to_string(),
                description: "Xray subscriptions and discovered nodes".to_string(),
                storage_type: "sqlite".to_string(),
                shadow_delete: false,
            })?;
        }
        Ok(SubscriptionStore {
            db: databases.interface(Options {
                local: true,
                internal: true,
                ..Options::default()
            }),
        })
    }

    pub fn list(&self) -> Result<Vec<Subscription>> {
        let query = Query::new(&format!("{XRAY_DB}:{SUBSCRIPTION_PREFIX}"));
        let mut subscriptions: Vec<Subscription> = self
            .db
            .query(&query)?
            .into_iter()
            .filter_map(|record| record.decode::<Subscription>().ok())
            .collect();
        subscriptions.sort_by(|a, b| a.name.cmp(&b.name).then(a.id.cmp(&b.id)));
        Ok(subscriptions)
    }

    pub fn get(&self, id: &str) -> Result<Subscription> {
        let record = self.db.get(&full_key(&subscription_key(id)))?;
        Ok(record.decode::<Subscription>()?)
    }

    pub fn add(&self, request: &AddSubscriptionRequest) -> Result<Subscription> {
        let trimmed = request.url.trim();
        if trimmed.is_empty() {
            return Err(Error::InvalidRequest("url or links is required".into()));
        }
        let id = id_for(trimmed);
        let name = if !request.name.trim().is_empty() {
            request.name.trim().to_string()
        } else if trimmed.starts_with("http://") || trimmed.starts_with("https://") {
            format!("Subscription {id}")
        } else if let Some(pos) = trimmed.find('#') {
            let fragment = &trimmed[pos + 1..];
            let tag = fragment.split_whitespace().next().unwrap_or(fragment);
            format!("Node: {tag}")
        } else {
            "Custom Links".to_string()
        };
        let subscription = Subscription {
            id: id.clone(),
            name,
            url: trimmed.to_string(),
            enabled: request.enabled,
            last_updated: 0,
            node_count: 0,
        };
        self.save(&subscription)?;
        Ok(subscription)
    }

    pub fn update(&self, request: &UpdateSubscriptionRequest) -> Result<Subscription> {
        if request.id.is_empty() {
            return Err(Error::InvalidRequest("id is required".into()));
        }
        let mut subscription = self.get(&request.id)?;
        if let Some(name) = &request.name {
            if name.trim().is_empty() {
                return Err(Error::InvalidRequest("name must not be empty".into()));
            }
            subscription.name = name.trim().to_string();
        }
        if let Some(url) = &request.url {
            if url.trim().is_empty() {
                return Err(Error::InvalidRequest("url must not be empty".into()));
            }
            subscription.url = url.trim().to_string();
        }
        if let Some(enabled) = request.enabled {
            subscription.enabled = enabled;
        }
        self.save(&subscription)?;
        Ok(subscription)
    }

    pub fn remove(&self, id: &str) -> Result<()> {
        self.get(id)?;
        self.remove_nodes(id)?;
        if self
            .active_raw()
            .and_then(|value| value.split_once(':').map(|(subscription, _)| subscription.to_string()))
            .as_deref()
            == Some(id)
        {
            self.clear_active();
        }
        self.db.delete(&full_key(&subscription_key(id)))?;
        Ok(())
    }

    /// Refreshes one or all enabled subscriptions and returns the number of
    /// discovered nodes.
    pub fn refresh(
        &self,
        id: &str,
        fetcher: &dyn SubscriptionFetcher,
        geoip: Option<&GeoIp>,
    ) -> Result<usize> {
        let subscriptions = if id.is_empty() {
            self.list()?
                .into_iter()
                .filter(|subscription| subscription.enabled)
                .collect::<Vec<_>>()
        } else {
            vec![self.get(id)?]
        };
        let mut total = 0;
        for mut subscription in subscriptions {
            let body = if !subscription.url.starts_with("http://") && !subscription.url.starts_with("https://") {
                subscription.url.clone()
            } else {
                match fetcher.fetch(&subscription.url) {
                    Ok(body) => body,
                    Err(error) => {
                        tracing::warn!(%error, id = %subscription.id, "xray: failed to refresh subscription");
                        continue;
                    }
                }
            };
            let links = match decode_payload(&body) {
                Ok(links) => links,
                Err(error) => {
                    tracing::warn!(%error, id = %subscription.id, "xray: failed to decode subscription payload");
                    continue;
                }
            };
            let mut nodes = Vec::new();
            for link in links {
                match parse_profile(&link, &subscription.id, geoip) {
                    Ok(node) => nodes.push(node),
                    Err(error) => tracing::debug!(%error, %link, "xray: skipping profile"),
                }
            }
            if !nodes.is_empty() {
                self.replace_nodes(&subscription.id, &nodes)?;
                subscription.last_updated = crate::now();
                subscription.node_count = nodes.len();
                total += nodes.len();
                self.save(&subscription)?;
            } else {
                tracing::warn!(id = %subscription.id, "xray: subscription yielded 0 valid nodes");
            }
        }
        Ok(total)
    }

    pub fn list_nodes(&self) -> Result<Vec<XrayNode>> {
        let query = Query::new(&format!("{XRAY_DB}:{NODE_PREFIX}"));
        let mut nodes: Vec<XrayNode> = self
            .db
            .query(&query)?
            .into_iter()
            .filter_map(|record| record.decode::<XrayNode>().ok())
            .collect();
        nodes.sort_by(|a, b| a.name.cmp(&b.name).then(a.id.cmp(&b.id)));
        Ok(nodes)
    }

    pub fn list_nodes_for_subscription(&self, subscription_id: &str) -> Result<Vec<XrayNode>> {
        let prefix = format!("{XRAY_DB}:{NODE_PREFIX}{subscription_id}:");
        let mut nodes: Vec<XrayNode> = self
            .db
            .query(&Query::new(&prefix))?
            .into_iter()
            .filter_map(|record| record.decode::<XrayNode>().ok())
            .collect();
        nodes.sort_by(|a, b| a.name.cmp(&b.name).then(a.id.cmp(&b.id)));
        Ok(nodes)
    }

    /// Deletes every node belonging to a subscription.
    pub fn remove_nodes(&self, subscription_id: &str) -> Result<()> {
        let prefix = format!("{XRAY_DB}:{NODE_PREFIX}{subscription_id}:");
        for record in self.db.query(&Query::new(&prefix))? {
            self.db.delete(&full_key(&record.key.key))?;
        }
        Ok(())
    }

    pub fn get_node(&self, id: &str) -> Result<XrayNode> {
        self.list_nodes()?
            .into_iter()
            .find(|node| node.id == id)
            .ok_or_else(|| Error::NodeNotFound(id.to_string()))
    }

    /// Replaces all nodes of a subscription, deleting nodes that disappeared.
    pub fn replace_nodes(&self, subscription_id: &str, nodes: &[XrayNode]) -> Result<()> {
        let prefix = format!("{XRAY_DB}:{NODE_PREFIX}{subscription_id}:");
        let old: Vec<String> = self
            .db
            .query(&Query::new(&prefix))?
            .into_iter()
            .map(|record| record.key.key)
            .collect();
        for node in nodes {
            let key = Key::new(XRAY_DB, &node_key(&node.subscription_id, &node.id));
            let record = Record::from_typed(key, Meta::default(), node)?;
            self.db.put_new(record)?;
        }
        let wanted: Vec<String> = nodes
            .iter()
            .map(|node| node_key(subscription_id, &node.id))
            .collect();
        for key in old {
            if !wanted.contains(&key) {
                self.db.delete(&format!("{XRAY_DB}:{key}"))?;
            }
        }
        Ok(())
    }

    pub fn set_active(&self, subscription_id: &str, node_id: &str) -> Result<()> {
        let record = Record::from_typed(
            Key::new(XRAY_DB, ACTIVE_KEY),
            Meta::default(),
            &format!("{subscription_id}:{node_id}"),
        )?;
        Ok(self.db.put_new(record)?)
    }

    fn active_raw(&self) -> Option<String> {
        self.db
            .get(&full_key(ACTIVE_KEY))
            .ok()
            .and_then(|record| record.decode::<String>().ok())
    }

    pub fn active_node_id(&self) -> Option<String> {
        self.active_raw()
            .and_then(|value| value.split_once(':').map(|(_, node)| node.to_string()))
    }

    pub fn clear_active(&self) {
        let _ = self.db.delete(&full_key(ACTIVE_KEY));
    }

    fn save(&self, subscription: &Subscription) -> Result<()> {
        let key = Key::new(XRAY_DB, &subscription_key(&subscription.id));
        let record = Record::from_typed(key, Meta::default(), subscription)?;
        Ok(self.db.put(record)?)
    }
}

/// Placeholder node ID helper re-exported for callers that need to correlate a
/// raw link with a stored node.
pub fn id_for_raw(subscription_id: &str, raw: &str) -> String {
    node_id(subscription_id, raw)
}

#[cfg(test)]
mod tests {
    use super::*;

    struct StaticFetcher(String);
    impl SubscriptionFetcher for StaticFetcher {
        fn fetch(&self, _url: &str) -> std::result::Result<String, String> {
            Ok(self.0.clone())
        }
    }

    fn store() -> (Arc<Databases>, SubscriptionStore) {
        let databases = Arc::new(Databases::without_disk());
        databases.register_injected(XRAY_DB, "test").unwrap();
        databases
            .inject(
                XRAY_DB,
                Box::new(pm_database::MemoryStorage::new()),
            )
            .unwrap();
        let store = SubscriptionStore::new(&databases).unwrap();
        (databases, store)
    }

    #[test]
    fn decodes_raw_wireguard_config_from_a_subscription() {
        let raw = "[Interface]\nPrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nAddress = 10.0.0.2/32\n[Peer]\nPublicKey = ISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0+P0A=\nEndpoint = wg.example.com:51820\nAllowedIPs = 0.0.0.0/0\n";
        let links = decode_payload(raw).unwrap();
        assert_eq!(links.len(), 1);
        let node = crate::parser::parse_profile(&links[0], "sub", None).unwrap();
        assert_eq!(node.protocol, crate::Protocol::Wireguard);
        assert_eq!(node.address, "wg.example.com");
        assert_eq!(node.port, 51820);
    }

    #[test]
    fn extracts_basic_auth_and_custom_headers_from_subscription_url() {
        let (url, headers) = subscription_request(
            "https://user:pass%40example@sub.example.com/sub?header-X-Token=abc%20123&type=base64",
        );
        assert_eq!(url, "https://sub.example.com/sub?type=base64");
        assert_eq!(
            headers,
            vec![
                (
                    "Authorization".to_string(),
                    format!("Basic {}", STANDARD.encode("user:pass@example")),
                ),
                ("X-Token".to_string(), "abc 123".to_string()),
            ]
        );
        let (plain, headers) = subscription_request("https://sub.example.com/sub");
        assert_eq!(plain, "https://sub.example.com/sub");
        assert!(headers.is_empty());
    }

    #[test]
    fn decodes_plain_and_base64() {
        let plain = "vless://id@example.com:443#One\nvmess://abc#Two";
        assert_eq!(decode_payload(plain).unwrap(), vec!["vless://id@example.com:443#One", "vmess://abc#Two"]);
        let encoded = STANDARD.encode(plain.as_bytes());
        assert_eq!(decode_payload(&encoded).unwrap(), decode_payload(plain).unwrap());
    }

    #[test]
    fn refreshes_and_lists_nodes() {
        let (_databases, store) = store();
        let subscription = store
            .add(&AddSubscriptionRequest {
                name: "Test".into(),
                url: "https://example.com/sub".into(),
                enabled: true,
            })
            .unwrap();
        let link = "vless://00010203-0405-0607-0809-0a0b0c0d0e0f@example.com:443#DE-Node";
        let encoded = STANDARD.encode(link.as_bytes());
        let fetcher = StaticFetcher(encoded);
        assert_eq!(store.refresh(&subscription.id, &fetcher, None).unwrap(), 1);
        let nodes = store.list_nodes().unwrap();
        assert_eq!(nodes.len(), 1);
        assert_eq!(nodes[0].country_code, "DE");
        assert_eq!(store.get(&subscription.id).unwrap().node_count, 1);
        store.remove(&subscription.id).unwrap();
        assert!(store.list_nodes().unwrap().is_empty());
    }
}
