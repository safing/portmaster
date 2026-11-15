//! API permissions, authentication tokens and authenticators.

use std::collections::HashMap;

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

/// A set of granted permissions.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AuthToken {
    pub read: Permission,
    pub write: Permission,
}

impl AuthToken {
    pub const fn new(read: Permission, write: Permission) -> AuthToken {
        AuthToken { read, write }
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

/// Parses API keys of the form `<key>?read=<perm>&write=<perm>`.
///
/// The Go format supports a URL scheme and an `expires` parameter; both are
/// accepted syntactically, but expiry is not enforced yet.
pub fn parse_api_keys(keys: &[String]) -> HashMap<String, AuthToken> {
    let mut tokens = HashMap::new();
    for key in keys {
        if let Some((path, token)) = parse_api_key(key) {
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
    for pair in query.split('&') {
        if pair.is_empty() {
            continue;
        }
        let (name, value) = pair.split_once('=').unwrap_or((pair, ""));
        match name {
            "read" => read = parse_permission(value)?,
            "write" => write = parse_permission(value)?,
            // "expires" is accepted but not enforced yet.
            _ => {}
        }
    }

    Some((path.to_string(), AuthToken::new(read, write)))
}

/// Extracts a bearer token from the Authorization header.
pub fn bearer_token(headers: &HeaderMap) -> Option<&str> {
    let value = headers.get("authorization")?.to_str().ok()?;
    value.strip_prefix("Bearer ")
}
