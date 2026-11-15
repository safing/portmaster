//! Validation of upstream proxy URLs (Go `proxy.ParseUpstreamProxyURL`).

use url::Url;

const SCHEMES: [&str; 5] = ["http", "socks4", "socks4a", "socks5", "socks5h"];

/// Returns whether the URL is an acceptable upstream proxy address.
pub fn validate_upstream_proxy_url(raw: &str) -> Result<(), String> {
    let url = Url::parse(raw).map_err(|_| "proxy URL is malformed".to_string())?;
    let scheme = url.scheme();
    if !SCHEMES.contains(&scheme) {
        return Err(format!(
            "unsupported proxy scheme {scheme:?}, supported are: {}",
            SCHEMES.join(", ")
        ));
    }
    if url.host_str().is_none_or(str::is_empty) {
        return Err("proxy URL is missing the host".to_string());
    }
    // The url crate hides an explicit default port (":80" for http).
    let explicit_default_port = scheme == "http" && authority_has_port_80(raw);
    if !explicit_default_port && url.port().is_none_or(|port| port == 0) {
        return Err("proxy URL is missing a valid port".to_string());
    }
    if !matches!(url.path(), "" | "/") || url.query().is_some() || url.fragment().is_some() {
        return Err("proxy URL must not contain a path, query or fragment".to_string());
    }
    let username = url.username();
    let password = url.password();
    if !username.is_empty() || password.is_some() {
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
        if username.len() > 255 || password.is_some_and(|p| p.len() > 255) {
            return Err("proxy username and password must be at most 255 bytes long".to_string());
        }
    }
    Ok(())
}

fn authority_has_port_80(raw: &str) -> bool {
    let Some((_, rest)) = raw.split_once("://") else {
        return false;
    };
    let authority = rest.split(['/', '?', '#']).next().unwrap_or("");
    let host_port = authority.rsplit('@').next().unwrap_or("");
    host_port.ends_with(":80")
}
