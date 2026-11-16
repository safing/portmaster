//! Optional Apprise notifications for core events.
//!
//! Go's core does not register Apprise itself (it is used by the observation
//! hub), but this port wires it in as an opt-in bridge: set `core/appriseURL`
//! to an Apprise API URL and the core sends online-status changes there.
//! With the default empty URL nothing is configured or sent.

use std::sync::Arc;

use pm_apprise::{Message, Notifier, TYPE_FAILURE, TYPE_SUCCESS, TYPE_WARNING};

/// Key of the optional Apprise API URL.
pub const CFG_APPRISE_URL: &str = "core/appriseURL";

/// Thin wrapper around the Apprise notifier.
pub struct Apprise {
    notifier: Option<Notifier>,
}

impl Apprise {
    /// Creates the bridge; an empty URL disables it.
    pub fn new(url: &str) -> Apprise {
        Apprise {
            notifier: (!url.is_empty()).then(|| Notifier::new(url)),
        }
    }

    /// Returns whether notifications are configured.
    pub fn is_enabled(&self) -> bool {
        self.notifier.is_some()
    }

    /// Sends one message, if configured. Errors are logged and do not affect
    /// the core.
    pub async fn send(&self, title: &str, body: &str, kind: &str) {
        let Some(notifier) = &self.notifier else {
            return;
        };
        let mut message = Message {
            title: title.to_string(),
            body: body.to_string(),
            r#type: kind.to_string(),
            ..Message::default()
        };
        if let Err(error) = notifier.send(&mut message).await {
            tracing::warn!(%error, "apprise: failed to send notification");
        }
    }

    /// Sends a notification about an online status change.
    pub async fn online_status(&self, status: pm_netenv::OnlineStatus) {
        use pm_netenv::OnlineStatus;
        let (title, kind) = match status {
            OnlineStatus::Offline => ("Portmaster is offline", TYPE_WARNING),
            OnlineStatus::Portal => ("Captive portal detected", TYPE_WARNING),
            OnlineStatus::Limited => ("Limited network connectivity", TYPE_WARNING),
            OnlineStatus::Online | OnlineStatus::SemiOnline => {
                ("Portmaster is online", TYPE_SUCCESS)
            }
            OnlineStatus::Unknown => ("Network status unknown", TYPE_FAILURE),
        };
        self.send(title, &format!("Online status: {status}"), kind)
            .await;
    }
}

/// Creates the bridge from the configured URL.
pub fn from_config(config: &std::sync::Arc<pm_config::Config>) -> Arc<Apprise> {
    let getter = config.get_as_string(CFG_APPRISE_URL, "");
    Arc::new(Apprise::new(&getter()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_url_disables_notifications() {
        let apprise = Apprise::new("");
        assert!(!apprise.is_enabled());

        let config = pm_config::Config::new();
        let bridge = from_config(&config);
        assert!(!bridge.is_enabled(), "unregistered option is empty");
    }

    #[test]
    fn configured_url_enables_notifications() {
        let apprise = Apprise::new("http://127.0.0.1:1/notify");
        assert!(apprise.is_enabled());
    }
}
