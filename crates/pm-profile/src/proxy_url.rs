//! Validation of upstream proxy URLs (Go `proxy.ParseUpstreamProxyURL`).

use url::{ParseError, Url};

const SCHEMES: [&str; 5] = ["http", "socks4", "socks4a", "socks5", "socks5h"];

fn unsupported_scheme(scheme: &str) -> String {
    format!(
        "unsupported proxy scheme {scheme:?}, supported are: {}",
        SCHEMES.join(", ")
    )
}

/// Percent-decodes like Go `url.QueryUnescape` without `+` handling; an
/// invalid escape is an error.
fn unescape(raw: &str) -> Option<Vec<u8>> {
    let bytes = raw.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' {
            let hex = raw.get(i + 1..i + 3)?;
            out.push(u8::from_str_radix(hex, 16).ok()?);
            i += 3;
        } else {
            out.push(bytes[i]);
            i += 1;
        }
    }
    Some(out)
}

/// Returns the authority part of a URL string.
fn authority(raw: &str) -> &str {
    let rest = raw.split_once("://").map_or("", |(_, rest)| rest);
    rest.split(['/', '?', '#']).next().unwrap_or("")
}

/// Returns whether the URL is an acceptable upstream proxy address.
pub fn validate_upstream_proxy_url(raw: &str) -> Result<(), String> {
    let url = match Url::parse(raw) {
        Ok(url) => url,
        // Go reads a URL without scheme as a path with an empty scheme,
        // unless the first segment contains a colon.
        Err(ParseError::RelativeUrlWithoutBase) => {
            let first = raw.split('/').next().unwrap_or("");
            if first.contains(':') {
                return Err("proxy URL is malformed".to_string());
            }
            return Err(unsupported_scheme(""));
        }
        Err(_) => return Err("proxy URL is malformed".to_string()),
    };
    let scheme = url.scheme();
    if !SCHEMES.contains(&scheme) {
        return Err(unsupported_scheme(scheme));
    }
    if url.host_str().is_none_or(str::is_empty) {
        return Err("proxy URL is missing the host".to_string());
    }

    // The url crate hides default ports (":80" for http), so read the port
    // from the raw authority.
    let authority = authority(raw);
    let (userinfo, host_port) = match authority.rsplit_once('@') {
        Some((userinfo, host_port)) => (Some(userinfo), host_port),
        None => (None, authority),
    };
    let port = if host_port.ends_with(']') {
        None
    } else {
        host_port.rsplit_once(':').map(|(_, port)| port)
    };
    match port.and_then(|p| p.parse::<u16>().ok()) {
        Some(port) if port != 0 => {}
        _ => return Err("proxy URL is missing a valid port".to_string()),
    }

    if !matches!(url.path(), "" | "/")
        || url.query().is_some_and(|q| !q.is_empty())
        || url.fragment().is_some_and(|f| !f.is_empty())
    {
        return Err("proxy URL must not contain a path, query or fragment".to_string());
    }

    if let Some(userinfo) = userinfo {
        let (user, password) = match userinfo.split_once(':') {
            Some((user, password)) => (user, Some(password)),
            None => (userinfo, None),
        };
        let username = unescape(user).ok_or("proxy URL is malformed")?;
        let password = match password {
            Some(p) => Some(unescape(p).ok_or("proxy URL is malformed")?),
            None => None,
        };
        let allows_password = matches!(scheme, "http" | "socks5" | "socks5h");
        if password.is_some() && !allows_password {
            let name = if scheme == "socks4a" {
                "SOCKS4a"
            } else {
                "SOCKS4"
            };
            return Err(format!(
                "{name} does not support password authentication, only a user ID"
            ));
        }
        if username.is_empty() {
            return Err("proxy username must not be empty".to_string());
        }
        let password = password.unwrap_or_default();
        if username.contains(&0) || password.contains(&0) {
            return Err("proxy username and password must not contain NUL characters".to_string());
        }
        if username.len() > 255 || password.len() > 255 {
            return Err("proxy username and password must be at most 255 bytes long".to_string());
        }
    }
    Ok(())
}
