//! API permissions, authentication tokens and authenticators.

use std::collections::HashMap;
use std::time::{SystemTime, UNIX_EPOCH};

use http::HeaderMap;

use crate::Request;

/// API permission values, matching the Go `Permission` type.
pub type Permission = i8;

pub const NOT_FOUND: Permission = -2;
pub const DYNAMIC: Permission = -1;
pub const NOT_SUPPORTED: Permission = 0;
pub const PERMIT_ANYONE: Permission = 1;
pub const PERMIT_USER: Permission = 2;
pub const PERMIT_ADMIN: Permission = 3;
pub const PERMIT_SELF: Permission = 4;

/// Returns the Go name of a permission.
pub fn permission_name(permission: Permission) -> &'static str {
    match permission {
        NOT_SUPPORTED => "NotSupported",
        DYNAMIC => "Dynamic",
        PERMIT_ANYONE => "PermitAnyone",
        PERMIT_USER => "PermitUser",
        PERMIT_ADMIN => "PermitAdmin",
        PERMIT_SELF => "PermitSelf",
        NOT_FOUND => "NotFound",
        _ => "Unknown",
    }
}

/// Returns the human-readable role of a permission, like Go `Permission.Role`.
pub fn permission_role(permission: Permission) -> &'static str {
    match permission {
        PERMIT_ANYONE => "Anyone",
        PERMIT_USER => "User",
        PERMIT_ADMIN => "Admin",
        PERMIT_SELF => "Self",
        _ => "Unknown",
    }
}

/// Parses a permission from a configuration string.
pub fn parse_permission(text: &str) -> Option<Permission> {
    match text.trim().to_lowercase().as_str() {
        "" | "anyone" => Some(PERMIT_ANYONE),
        "user" => Some(PERMIT_USER),
        "admin" => Some(PERMIT_ADMIN),
        _ => None,
    }
}

/// Identity of the process that was authenticated for an API request.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthProcess {
    /// PID of the calling process.
    pub pid: i32,
    /// Path of its executable.
    pub path: String,
}

/// A set of granted permissions.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthToken {
    pub read: Permission,
    pub write: Permission,
    /// Process identity, when the authenticator identified the caller.
    pub process: Option<AuthProcess>,
    /// Unix timestamp (seconds) until which this token is valid, if limited.
    /// Corresponds to Go's `AuthToken.ValidUntil` (`*time.Time`).
    pub valid_until: Option<i64>,
}

impl AuthToken {
    /// Attaches the authenticated process identity.
    #[must_use]
    pub fn with_process(mut self, process: AuthProcess) -> AuthToken {
        self.process = Some(process);
        self
    }

    pub const fn new(read: Permission, write: Permission) -> AuthToken {
        AuthToken {
            read,
            write,
            process: None,
            valid_until: None,
        }
    }

    /// Returns whether the token has expired, matching Go's
    /// `time.Now().After(*token.ValidUntil)` check.
    pub fn expired(&self) -> bool {
        self.valid_until.is_some_and(|until| now_unix() > until)
    }

    /// A token that is not authenticated.
    pub const fn public() -> AuthToken {
        AuthToken::new(PERMIT_ANYONE, PERMIT_ANYONE)
    }

    /// A token with all permissions.
    pub const fn full() -> AuthToken {
        AuthToken::new(PERMIT_SELF, PERMIT_SELF)
    }

    /// Checks whether the token is authenticated at all.
    pub fn is_authenticated(&self) -> bool {
        self.read != PERMIT_ANYONE || self.write != PERMIT_ANYONE
    }
}

/// Why an authenticator rejected a request.
#[derive(Debug, Clone)]
pub enum AuthError {
    /// Access is denied; the message is shown to the user when access is
    /// required.
    Denied(String),
    /// The authenticator itself failed.
    Internal(String),
}

/// Authenticates requests, like the Go `AuthenticatorFunc`.
pub trait Authenticator: Send + Sync {
    /// Returns a token when the request could be authenticated. `Ok(None)`
    /// means "not authenticated" without a user-facing error.
    fn authenticate(&self, request: &Request) -> Result<Option<AuthToken>, AuthError>;
}

/// Parses API keys of the form `<key>?read=<perm>&write=<perm>&expires=<rfc3339>`.
///
/// Like Go's `updateAPIKeys`, the key string may carry a URL scheme and an
/// `expires` parameter. Keys with an invalid or already-expired expiry are
/// dropped so they can never authorize a request.
pub fn parse_api_keys(keys: &[String]) -> HashMap<String, AuthToken> {
    let mut tokens = HashMap::new();
    for key in keys {
        if let Some((path, token)) = parse_api_key(key) {
            if token.expired() {
                continue;
            }
            tokens.insert(path, token);
        }
    }
    tokens
}

fn parse_api_key(key: &str) -> Option<(String, AuthToken)> {
    let (address, query) = match key.split_once('?') {
        Some((address, query)) => (address, query),
        None => (key, ""),
    };

    // Reduce a full URL to its path.
    let path = match address.split_once("://") {
        Some((_, rest)) => match rest.find('/') {
            Some(index) => &rest[index..],
            None => return None,
        },
        None => address,
    };
    if path.is_empty() {
        return None;
    }

    let mut read = PERMIT_ANYONE;
    let mut write = PERMIT_ANYONE;
    let mut valid_until = None;
    for pair in query.split('&') {
        if pair.is_empty() {
            continue;
        }
        let (name, value) = pair.split_once('=').unwrap_or((pair, ""));
        match name {
            "read" => read = parse_permission(value)?,
            "write" => write = parse_permission(value)?,
            "expires" if !value.is_empty() => {
                // An unparseable expiry invalidates the key, like Go's
                // `time.Parse(time.RFC3339, ...)` error path.
                valid_until = Some(pm_dsd::gotime::parse_rfc3339(value)?);
            }
            _ => {}
        }
    }

    let mut token = AuthToken::new(read, write);
    token.valid_until = valid_until;
    Some((path.to_string(), token))
}

/// Extracts an API key from the Authorization header.
///
/// Mirrors Go's `checkAPIKey`: `Bearer <key>` is used verbatim, while
/// `Basic <base64(user:pass)>` maps to the concatenation `user+pass`.
pub fn bearer_token(headers: &HeaderMap) -> Option<String> {
    let value = headers.get("authorization")?.to_str().ok()?;
    if let Some(key) = value.strip_prefix("Bearer ") {
        return Some(key.to_string());
    }
    if let Some(encoded) = value.strip_prefix("Basic ") {
        let decoded = decode_base64(encoded)?;
        let decoded = std::str::from_utf8(&decoded).ok()?;
        // Go's `r.BasicAuth()` splits on the first colon and ignores the
        // `ok` flag, so the lookup key is simply the two halves joined.
        let (user, pass) = decoded.split_once(':')?;
        return Some(format!("{user}{pass}"));
    }
    None
}

/// Current Unix time in seconds.
fn now_unix() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs() as i64)
        .unwrap_or(0)
}

/// Minimal standard base64 decoder (Go `base64.StdEncoding.DecodeString`).
fn decode_base64(input: &str) -> Option<Vec<u8>> {
    let bytes = input.as_bytes();
    if !bytes.len().is_multiple_of(4) {
        return None;
    }
    let mut output = Vec::with_capacity(bytes.len() / 4 * 3);
    for group in bytes.chunks(4) {
        let padding = group.iter().filter(|&&byte| byte == b'=').count();
        if padding > 2 || group[..4 - padding].contains(&b'=') {
            return None;
        }
        let mut values = [0u8; 4];
        for (index, &byte) in group.iter().enumerate() {
            values[index] = match byte {
                b'A'..=b'Z' => byte - b'A',
                b'a'..=b'z' => byte - b'a' + 26,
                b'0'..=b'9' => byte - b'0' + 52,
                b'+' => 62,
                b'/' => 63,
                b'=' => 0,
                _ => return None,
            };
        }
        output.push((values[0] << 2) | (values[1] >> 4));
        if padding < 2 {
            output.push((values[1] << 4) | (values[2] >> 2));
        }
        if padding == 0 {
            output.push((values[2] << 6) | values[3]);
        }
    }
    Some(output)
}
