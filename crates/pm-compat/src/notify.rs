//! Notification definitions and the pure notification logic of Go
//! `service/compat/notify.go`.
//!
//! Event IDs, titles, messages, levels and actions are kept byte-for-byte from
//! the Go implementation. The rate-limiting threshold is a pure function of a
//! `HashMap` and an `Instant`, which makes it straightforward to test.

use std::collections::HashMap;
use std::time::{Duration, Instant};

use pm_notifications::{Action, Notification, ACTION_TYPE_OPEN_SETTING, TYPE_ERROR, TYPE_WARNING};

/// Copy of `firewall.CfgOptionDNSQueryInterceptionKey`.
pub const CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY: &str = "filter/dnsQueryInterception";

// -- System issues ---------------------------------------------------------

pub const SYSTEM_INTEGRATION_ISSUE_ID: &str = "compat:system-integration-issue";
pub const SYSTEM_INTEGRATION_ISSUE_TITLE: &str = "Detected System Integration Issue";
pub const SYSTEM_INTEGRATION_ISSUE_MESSAGE: &str = "Portmaster detected a problem with its system integration. You can try to restart or reinstall the Portmaster. If that does not help, [get support here](https://safing.io/support/).";

pub const SYSTEM_COMPATIBILITY_ISSUE_ID: &str = "compat:compatibility-issue";
pub const SYSTEM_COMPATIBILITY_ISSUE_TITLE: &str = "Detected Compatibility Issue";
pub const SYSTEM_COMPATIBILITY_ISSUE_MESSAGE: &str = "Portmaster detected that something is interfering with its operation. This could be a VPN, an Anti-Virus or another network protection software. Please check if you are running an incompatible [VPN client](https://docs.safing.io/portmaster/install/status/vpn-compatibility) or [software](https://docs.safing.io/portmaster/install/status/software-compatibility) and disable it. If that does not help, [get support here](https://safing.io/support/).";

pub const MANUAL_DNS_SETUP_REQUIRED_ID: &str = "compat:manual-dns-setup-required";
pub const MANUAL_DNS_SETUP_REQUIRED_TITLE: &str = "Manual DNS Setup Required";
pub const MANUAL_DNS_SETUP_REQUIRED_MESSAGE: &str = "You have disabled Seamless DNS Integration. As a result, Portmaster can no longer protect you or filter connections reliably. To fix this, you have to manually configure %s as the DNS Server in your system and in any conflicting application. This message will disappear some time after correct configuration.";

// -- App issues ------------------------------------------------------------

pub const SECURE_DNS_BYPASS_ID: &str = "compat:secure-dns-bypass-%s";
pub const SECURE_DNS_BYPASS_TITLE: &str = "Blocked Bypass Attempt by %s";
pub const SECURE_DNS_BYPASS_MESSAGE: &str = "[APPNAME] is using its own Secure DNS resolver, which would bypass Portmaster's firewall protections. If [APPNAME] experiences problems, disable Secure DNS within [APPNAME] to restore functionality. Rest assured that Portmaster handles Secure DNS for your whole device, including [APPNAME].";

pub const MULTI_PEER_UDP_TUNNEL_ID: &str = "compat:multi-peer-udp-tunnel-%s";
pub const MULTI_PEER_UDP_TUNNEL_TITLE: &str = "Detected SPN Incompatibility in %s";
pub const MULTI_PEER_UDP_TUNNEL_MESSAGE: &str = "Portmaster detected that [APPNAME] is trying to connect to multiple servers via the SPN using a single UDP connection. This is common for technologies such as torrents. Unfortunately, the SPN does not support this feature currently. You can try to change this behavior within the affected app or you could exempt it from using the SPN.";

// -- Thresholds ------------------------------------------------------------

pub const NOTIFY_THRESHOLD_MIN_INCIDENTS: u32 = 10;
pub const NOTIFY_THRESHOLD_RESET_AFTER: Duration = Duration::from_secs(2 * 60);

/// A system-level issue detected by the self-check.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SystemIssueKind {
    SystemIntegration,
    SystemCompatibility,
    ManualDnsSetup,
}

impl SystemIssueKind {
    pub fn id(self) -> &'static str {
        match self {
            SystemIssueKind::SystemIntegration => SYSTEM_INTEGRATION_ISSUE_ID,
            SystemIssueKind::SystemCompatibility => SYSTEM_COMPATIBILITY_ISSUE_ID,
            SystemIssueKind::ManualDnsSetup => MANUAL_DNS_SETUP_REQUIRED_ID,
        }
    }

    pub fn title(self) -> &'static str {
        match self {
            SystemIssueKind::SystemIntegration => SYSTEM_INTEGRATION_ISSUE_TITLE,
            SystemIssueKind::SystemCompatibility => SYSTEM_COMPATIBILITY_ISSUE_TITLE,
            SystemIssueKind::ManualDnsSetup => MANUAL_DNS_SETUP_REQUIRED_TITLE,
        }
    }

    /// Renders the issue message. Only the manual DNS setup message contains a
    /// format placeholder (the nameserver IP, quoted like Go's `%s` with the
    /// explicitly quoted address passed by `SetNameserverListenIP`).
    pub fn message(self, nameserver_ip: &str) -> String {
        match self {
            SystemIssueKind::SystemIntegration => SYSTEM_INTEGRATION_ISSUE_MESSAGE.to_string(),
            SystemIssueKind::SystemCompatibility => {
                SYSTEM_COMPATIBILITY_ISSUE_MESSAGE.to_string()
            }
            SystemIssueKind::ManualDnsSetup => MANUAL_DNS_SETUP_REQUIRED_MESSAGE
                .replace("%s", &format!("\"{nameserver_ip}\"")),
        }
    }

    pub fn level(self) -> pm_notifications::NotificationType {
        TYPE_ERROR
    }
}

/// An issue tied to a single application.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AppIssueKind {
    SecureDnsBypass,
    MultiPeerUdpTunnel,
}

impl AppIssueKind {
    fn id_template(self) -> &'static str {
        match self {
            AppIssueKind::SecureDnsBypass => SECURE_DNS_BYPASS_ID,
            AppIssueKind::MultiPeerUdpTunnel => MULTI_PEER_UDP_TUNNEL_ID,
        }
    }

    fn title_template(self) -> &'static str {
        match self {
            AppIssueKind::SecureDnsBypass => SECURE_DNS_BYPASS_TITLE,
            AppIssueKind::MultiPeerUdpTunnel => MULTI_PEER_UDP_TUNNEL_TITLE,
        }
    }

    fn message_template(self) -> &'static str {
        match self {
            AppIssueKind::SecureDnsBypass => SECURE_DNS_BYPASS_MESSAGE,
            AppIssueKind::MultiPeerUdpTunnel => MULTI_PEER_UDP_TUNNEL_MESSAGE,
        }
    }

    pub fn level(self) -> pm_notifications::NotificationType {
        TYPE_WARNING
    }

    pub fn event_id(self, profile_id: &str) -> String {
        self.id_template().replace("%s", profile_id)
    }

    pub fn title(self, app_name: &str) -> String {
        self.title_template().replace("%s", app_name)
    }

    pub fn message(self, app_name: &str) -> String {
        self.message_template().replace("[APPNAME]", app_name)
    }

    /// Mirrors Go's log label computation:
    /// `strings.ReplaceAll(strings.TrimPrefix(strings.TrimSuffix(id, "-%s"),
    /// "compat:"), "-", " ")`.
    pub fn display_name(self) -> String {
        display_name(self.id_template())
    }
}

fn display_name(id_template: &str) -> String {
    let trimmed = id_template
        .strip_suffix("-%s")
        .unwrap_or(id_template)
        .strip_prefix("compat:")
        .unwrap_or(id_template);
    trimmed.replace('-', " ")
}

/// Builds the notification for a system issue. Pure function; the caller owns
/// sending it through the notification registry.
pub fn system_issue_notification(kind: SystemIssueKind, nameserver_ip: &str) -> Notification {
    let mut notification = Notification::new(
        kind.level(),
        kind.id(),
        kind.title(),
        &kind.message(nameserver_ip),
        true,
    );
    if kind == SystemIssueKind::ManualDnsSetup {
        notification = notification.with_action(revert_setting_action());
    }
    notification
}

fn revert_setting_action() -> Action {
    Action::new("", "Revert").with_type(
        ACTION_TYPE_OPEN_SETTING,
        serde_json::json!({
            "Key": CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY,
            "Profile": "",
        }),
    )
}

/// Adds the default acknowledgement action used for app issues that define no
/// actions of their own (Go `appIssue.notify`).
pub fn app_issue_notification(kind: AppIssueKind, profile_id: &str, app_name: &str) -> Notification {
    Notification::new(
        kind.level(),
        &kind.event_id(profile_id),
        &kind.title(app_name),
        &kind.message(app_name),
        true,
    )
    .with_action(Action::new("ack", "OK"))
}

/// Tracks incidents of one event ID for the notification rate limiter.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NotifyThreshold {
    pub first_seen: Instant,
    pub incidents: u32,
}

impl NotifyThreshold {
    /// Go: `time.Now().Add(-resetAfter).After(FirstSeen)`.
    pub fn expired(&self, now: Instant) -> bool {
        now.checked_duration_since(self.first_seen)
            .is_some_and(|elapsed| elapsed > NOTIFY_THRESHOLD_RESET_AFTER)
    }
}

/// Go `isOverThreshold`: increments an existing, unexpired counter or starts a
/// fresh one (which never passes on its first incident).
pub fn is_over_threshold(
    thresholds: &mut HashMap<String, NotifyThreshold>,
    id: &str,
    now: Instant,
) -> bool {
    match thresholds.get_mut(id) {
        Some(threshold) if !threshold.expired(now) => {
            threshold.incidents += 1;
            threshold.incidents >= NOTIFY_THRESHOLD_MIN_INCIDENTS
        }
        _ => {
            thresholds.insert(
                id.to_string(),
                NotifyThreshold {
                    first_seen: now,
                    incidents: 1,
                },
            );
            false
        }
    }
}

/// Go `cleanNotifyThreshold`: drops expired rate-limit entries.
pub fn clean_notify_thresholds(thresholds: &mut HashMap<String, NotifyThreshold>, now: Instant) {
    thresholds.retain(|_, threshold| !threshold.expired(now));
}

#[cfg(test)]
mod tests {
    use super::*;
    use pm_notifications::{ACTION_TYPE_OPEN_SETTING, TYPE_ERROR, TYPE_WARNING};

    #[test]
    fn system_issue_ids_titles_and_levels_match_go() {
        let cases = [
            (
                SystemIssueKind::SystemIntegration,
                "compat:system-integration-issue",
                "Detected System Integration Issue",
            ),
            (
                SystemIssueKind::SystemCompatibility,
                "compat:compatibility-issue",
                "Detected Compatibility Issue",
            ),
            (
                SystemIssueKind::ManualDnsSetup,
                "compat:manual-dns-setup-required",
                "Manual DNS Setup Required",
            ),
        ];
        for (kind, id, title) in cases {
            let n = system_issue_notification(kind, "127.0.0.1");
            assert_eq!(n.event_id, id);
            assert_eq!(n.title, title);
            assert_eq!(n.type_, TYPE_ERROR);
            assert!(n.show_on_system);
        }
    }

    #[test]
    fn manual_dns_message_embeds_quoted_nameserver() {
        let n = system_issue_notification(SystemIssueKind::ManualDnsSetup, "127.0.0.42");
        assert_eq!(
            n.message,
            MANUAL_DNS_SETUP_REQUIRED_MESSAGE.replace("%s", "\"127.0.0.42\"")
        );
        assert_eq!(n.available_actions.len(), 1);
        let action = &n.available_actions[0];
        assert_eq!(action.text, "Revert");
        assert_eq!(action.type_, ACTION_TYPE_OPEN_SETTING);
        assert_eq!(action.payload["Key"], CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY);
    }

    #[test]
    fn other_system_issues_have_no_actions() {
        for kind in [
            SystemIssueKind::SystemIntegration,
            SystemIssueKind::SystemCompatibility,
        ] {
            assert!(system_issue_notification(kind, "127.0.0.1")
                .available_actions
                .is_empty());
        }
    }

    #[test]
    fn app_issue_ids_messages_and_ack_action_match_go() {
        let n = app_issue_notification(AppIssueKind::SecureDnsBypass, "local/app", "Firefox");
        assert_eq!(n.event_id, "compat:secure-dns-bypass-local/app");
        assert_eq!(n.title, "Blocked Bypass Attempt by Firefox");
        assert_eq!(n.type_, TYPE_WARNING);
        assert!(!n.message.contains("[APPNAME]"));
        assert!(n.message.starts_with("Firefox is using its own Secure DNS resolver"));
        assert_eq!(n.available_actions.len(), 1);
        assert_eq!(n.available_actions[0].id, "ack");
        assert_eq!(n.available_actions[0].text, "OK");

        let n = app_issue_notification(AppIssueKind::MultiPeerUdpTunnel, "local/tor", "Tor");
        assert_eq!(n.event_id, "compat:multi-peer-udp-tunnel-local/tor");
        assert_eq!(n.title, "Detected SPN Incompatibility in Tor");
    }

    #[test]
    fn app_issue_display_name_matches_go_transform() {
        assert_eq!(
            AppIssueKind::SecureDnsBypass.display_name(),
            "secure dns bypass"
        );
        assert_eq!(
            AppIssueKind::MultiPeerUdpTunnel.display_name(),
            "multi peer udp tunnel"
        );
    }

    #[test]
    fn threshold_requires_ten_incidents_within_two_minutes() {
        let mut thresholds = HashMap::new();
        let start = Instant::now();
        for i in 1..NOTIFY_THRESHOLD_MIN_INCIDENTS {
            assert!(!is_over_threshold(&mut thresholds, "id", start));
            assert_eq!(thresholds["id"].incidents, i);
        }
        assert!(is_over_threshold(&mut thresholds, "id", start));
    }

    #[test]
    fn threshold_expires_after_reset_after() {
        let mut thresholds = HashMap::new();
        let start = Instant::now();
        assert!(!is_over_threshold(&mut thresholds, "id", start));
        assert!(!is_over_threshold(
            &mut thresholds,
            "id",
            start + NOTIFY_THRESHOLD_RESET_AFTER - Duration::from_secs(1)
        ));
        // Expired: a fresh counter is created and does not pass.
        assert!(!is_over_threshold(
            &mut thresholds,
            "id",
            start + NOTIFY_THRESHOLD_RESET_AFTER + Duration::from_secs(1)
        ));
        assert_eq!(thresholds["id"].incidents, 1);
    }

    #[test]
    fn clean_removes_only_expired_entries() {
        let mut thresholds = HashMap::new();
        let start = Instant::now();
        let clean_at = start + NOTIFY_THRESHOLD_RESET_AFTER + Duration::from_secs(1);
        thresholds.insert(
            "fresh".to_string(),
            NotifyThreshold {
                first_seen: clean_at - Duration::from_secs(5),
                incidents: 3,
            },
        );
        thresholds.insert(
            "stale".to_string(),
            NotifyThreshold {
                first_seen: start,
                incidents: 9,
            },
        );
        clean_notify_thresholds(&mut thresholds, clean_at);
        assert!(thresholds.contains_key("fresh"));
        assert!(!thresholds.contains_key("stale"));
    }
}
