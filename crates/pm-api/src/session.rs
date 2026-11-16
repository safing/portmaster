//! API sessions, backed by the `Portmaster-API-Token` cookie.
//!
//! Like in Go, a successful external authentication issues a short-lived
//! session; the browser then sends the cookie instead of credentials. Session
//! keys are 256 bits from the operating system's random source.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use crate::permission::AuthToken;

/// Name of the session cookie.
pub const SESSION_COOKIE_NAME: &str = "Portmaster-API-Token";
/// Lifetime of a session, refreshed on every use.
pub const SESSION_COOKIE_TTL: Duration = Duration::from_secs(5 * 60);

struct Session {
    token: AuthToken,
    valid_until: Instant,
}

/// The session store of an API instance.
pub struct Sessions {
    ttl: Duration,
    sessions: Mutex<HashMap<String, Session>>,
}

impl Default for Sessions {
    fn default() -> Self {
        Sessions::new()
    }
}

impl Sessions {
    pub fn new() -> Sessions {
        Sessions::with_ttl(SESSION_COOKIE_TTL)
    }

    pub fn with_ttl(ttl: Duration) -> Sessions {
        Sessions {
            ttl,
            sessions: Mutex::new(HashMap::new()),
        }
    }

    /// Creates a session and returns its cookie value.
    pub fn create(&self, token: AuthToken) -> Option<String> {
        let mut key = [0u8; 32];
        if getrandom::fill(&mut key).is_err() {
            return None;
        }
        let value = hex::encode(key);
        self.sessions.lock().expect("lock poisoned").insert(
            value.clone(),
            Session {
                token,
                valid_until: Instant::now() + self.ttl,
            },
        );
        Some(value)
    }

    /// Returns the token of a valid session and refreshes it.
    pub fn check(&self, value: &str) -> Option<AuthToken> {
        let mut sessions = self.sessions.lock().expect("lock poisoned");
        let session = sessions.get_mut(value)?;
        if Instant::now() > session.valid_until {
            sessions.remove(value);
            return None;
        }
        session.valid_until = Instant::now() + self.ttl;
        Some(session.token.clone())
    }

    /// Deletes a session.
    pub fn delete(&self, value: &str) {
        self.sessions.lock().expect("lock poisoned").remove(value);
    }

    /// Removes expired sessions.
    pub fn clean(&self) {
        let now = Instant::now();
        self.sessions
            .lock()
            .expect("lock poisoned")
            .retain(|_, session| now <= session.valid_until);
    }

    /// Returns the number of active sessions.
    pub fn len(&self) -> usize {
        self.sessions.lock().expect("lock poisoned").len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

/// Extracts a cookie value from a `Cookie` header.
pub fn cookie_value(header: &str, name: &str) -> Option<String> {
    for pair in header.split(';') {
        let pair = pair.trim();
        let (key, value) = pair.split_once('=')?;
        if key.trim() == name {
            return Some(value.trim().to_string());
        }
    }
    None
}

/// Returns the `Set-Cookie` value for a new session.
pub fn session_cookie(value: &str) -> String {
    format!("{SESSION_COOKIE_NAME}={value}; Path=/; HttpOnly; SameSite=Strict")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::permission::{AuthToken, PERMIT_ADMIN, PERMIT_ANYONE};

    #[test]
    fn sessions_expire_and_refresh() {
        let sessions = Sessions::with_ttl(Duration::from_millis(50));
        let token = AuthToken::new(PERMIT_ADMIN, PERMIT_ADMIN);
        let value = sessions.create(token.clone()).unwrap();
        assert_eq!(value.len(), 64);
        assert_eq!(sessions.check(&value), Some(token.clone()));
        assert_eq!(sessions.len(), 1);

        std::thread::sleep(Duration::from_millis(60));
        assert_eq!(sessions.check(&value), None);
        assert!(sessions.is_empty());
    }

    #[test]
    fn sessions_are_unique_and_deletable() {
        let sessions = Sessions::new();
        let token = AuthToken::new(PERMIT_ANYONE, PERMIT_ANYONE);
        let first = sessions.create(token.clone()).unwrap();
        let second = sessions.create(token.clone()).unwrap();
        assert_ne!(first, second);
        sessions.delete(&first);
        assert_eq!(sessions.check(&first), None);
        assert_eq!(sessions.check(&second), Some(token));
    }

    #[test]
    fn cookie_parsing() {
        let header = "other=1; Portmaster-API-Token=abc123; another=2";
        assert_eq!(
            cookie_value(header, SESSION_COOKIE_NAME).as_deref(),
            Some("abc123")
        );
        assert_eq!(cookie_value("other=1", SESSION_COOKIE_NAME), None);
        assert_eq!(
            session_cookie("abc"),
            "Portmaster-API-Token=abc; Path=/; HttpOnly; SameSite=Strict"
        );
    }
}
