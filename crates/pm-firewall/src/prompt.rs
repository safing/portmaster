//! Connection prompts, port of `service/firewall/prompt.go`.
//!
//! When the default action is "ask", a prompt notification lets the user allow
//! or block the connection. The answer is saved as an endpoint rule in the
//! local profile, so the same decision applies to later connections of the app.
//!
//! # Compatibility notes
//!
//! * Waiting uses a `watch` channel and a `CancellationToken` instead of Go's
//!   channel `select`.
//! * The environment supplies the timeout, the notifications registry and the
//!   local profile; saving and reloading the profile is the caller's job.
//! * Replacing an existing prompt does not delete its notification, because the
//!   handle has no reference to the registry; the caller cleans up.

use std::sync::{Arc, LazyLock, Mutex};
use std::time::Duration;

use std::net::IpAddr;

use pm_network::{entity_to_json, Connection};
use pm_notifications::{Notification, NotificationHandle, Notifications, STATE_ACTIVE};
use serde_json::json;
use tokio_util::sync::CancellationToken;

/// Action ID: allow the domain and all its subdomains.
pub const ALLOW_DOMAIN_ALL: &str = "allow-domain-all";
/// Action ID: allow only the exact domain.
pub const ALLOW_DOMAIN_DISTINCT: &str = "allow-domain-distinct";
/// Action ID: block the domain and all its subdomains.
pub const BLOCK_DOMAIN_ALL: &str = "block-domain-all";
/// Action ID: block only the exact domain.
pub const BLOCK_DOMAIN_DISTINCT: &str = "block-domain-distinct";
/// Action ID: allow the IP address of an outgoing connection.
pub const ALLOW_IP: &str = "allow-ip";
/// Action ID: block the IP address of an outgoing connection.
pub const BLOCK_IP: &str = "block-ip";
/// Action ID: allow the IP address of an incoming connection.
pub const ALLOW_SERVING_IP: &str = "allow-serving-ip";
/// Action ID: block the IP address of an incoming connection.
pub const BLOCK_SERVING_IP: &str = "block-serving-ip";
/// Action ID of a dismissed prompt.
pub const CANCEL_PROMPT: &str = "cancel";

/// Prefix of prompt event IDs. Also used by the UI, do not change.
pub const PROMPT_ID_PREFIX: &str = "filter:prompt";

/// How long the filter waits for a prompt answer before continuing async.
pub const DECISION_TIMEOUT_SECONDS: i64 = 10;

/// Serializes prompt creation, like Go `promptNotificationCreation`.
static PROMPT_NOTIFICATION_CREATION: LazyLock<Mutex<()>> = LazyLock::new(|| Mutex::new(()));

/// The profile data shown in a prompt.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PromptProfile {
    /// Profile source (`local`).
    pub source: String,
    /// Profile ID.
    pub id: String,
    /// Profile name, used in the prompt message only.
    pub name: String,
    /// Path shown in the prompt, usually the executable path.
    pub linked_path: String,
}

/// The remote side of a prompt, detached from the entity borrow.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PromptTarget {
    /// Domain of the connection, empty for direct IP connections.
    pub domain: String,
    /// IP address of the connection.
    pub ip: Option<IpAddr>,
    /// IP protocol number.
    pub protocol: u8,
    /// Remote port.
    pub port: u16,
}

impl PromptTarget {
    /// Extracts the target data from a connection.
    pub fn from_connection(conn: &Connection) -> Option<PromptTarget> {
        let entity = conn.entity.as_ref()?;
        Some(PromptTarget {
            domain: entity.domain.clone(),
            ip: entity.ip,
            protocol: entity.protocol,
            port: entity.port,
        })
    }
}

/// Everything the prompt needs from the core.
pub trait PromptEnv: Send + Sync {
    /// Go `askTimeout`: seconds to wait for an answer.
    fn ask_timeout(&self) -> i64;
    /// Go `askWithSystemNotifications`.
    fn ask_with_system_notifications(&self) -> bool;
    /// Returns the notification registry.
    fn notifications(&self) -> Arc<Notifications>;
    /// Returns the local profile of the connection's process.
    fn local_profile(&self, conn: &Connection) -> Option<PromptProfile>;
    /// Saves an endpoint rule for the profile and reloads it.
    ///
    /// `entity` is the connection's entity, `action` one of the action IDs.
    /// The implementation builds or applies the rule through
    /// [`endpoint_rule`].
    fn save_response(
        &self,
        profile_id: &str,
        domain: &str,
        ip: Option<IpAddr>,
        action: &str,
    ) -> Result<(), String>;
}

/// What one prompt did.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PromptOutcome {
    /// The user allowed the connection.
    Allowed,
    /// The user blocked the connection.
    Blocked,
    /// The prompt was dismissed; wait for a new decision.
    Dismissed,
    /// No answer in time; the connection is denied and the prompt stays open.
    Timeout,
    /// The core is shutting down.
    Shutdown,
    /// No prompt was created (no profile, or an earlier answer was applied).
    NoPrompt,
}

/// Asks the user about the connection, setting its verdict.
pub async fn prompt(
    conn: &mut Connection,
    env: Arc<dyn PromptEnv>,
    shutdown: &CancellationToken,
) -> PromptOutcome {
    let Some(handle) = create_prompt(conn, Arc::clone(&env)) else {
        return PromptOutcome::NoPrompt;
    };

    conn.set_prompt(Arc::clone(&handle));

    // The decision timeout never exceeds the ask timeout.
    let mut timeout = DECISION_TIMEOUT_SECONDS;
    if timeout > env.ask_timeout() {
        timeout = env.ask_timeout();
    }
    let timeout = Duration::from_secs(timeout.max(1) as u64);

    let mut response = handle.response();
    if let Some(action) = response.borrow().clone() {
        return apply_response(conn, &action);
    }

    tokio::select! {
        changed = response.changed() => {
            if changed.is_err() {
                return PromptOutcome::NoPrompt;
            }
            let action = response.borrow().clone().unwrap_or_default();
            apply_response(conn, &action)
        }
        () = tokio::time::sleep(timeout) => {
            tracing::debug!("filter: continuing prompting async");
            conn.deny(
                "prompting in progress, please respond to prompt",
                pm_profile::CFG_OPTION_DEFAULT_ACTION_KEY,
            );
            PromptOutcome::Timeout
        }
        () = shutdown.cancelled() => {
            tracing::debug!("filter: aborting prompting because of shutdown");
            conn.drop_conn("shutting down", "");
            PromptOutcome::Shutdown
        }
    }
}

/// Applies the user's answer to the connection.
fn apply_response(conn: &mut Connection, action: &str) -> PromptOutcome {
    match action {
        ALLOW_DOMAIN_ALL | ALLOW_DOMAIN_DISTINCT | ALLOW_IP | ALLOW_SERVING_IP => {
            conn.accept("allowed via prompt", pm_profile::CFG_OPTION_ENDPOINTS_KEY);
            PromptOutcome::Allowed
        }
        "" => {
            // Dismissed without a decision.
            conn.deny(
                "prompting canceled, waiting for new decision",
                pm_profile::CFG_OPTION_DEFAULT_ACTION_KEY,
            );
            PromptOutcome::Dismissed
        }
        _ => {
            conn.deny("blocked via prompt", pm_profile::CFG_OPTION_ENDPOINTS_KEY);
            PromptOutcome::Blocked
        }
    }
}

/// Creates the prompt notification of the connection.
///
/// Returns `None` when no further action should be taken: the process has no
/// local profile, or a previous prompt was already answered.
pub fn create_prompt(
    conn: &mut Connection,
    env: Arc<dyn PromptEnv>,
) -> Option<Arc<NotificationHandle>> {
    let expires = now() + env.ask_timeout();
    let profile = env.local_profile(conn).or_else(|| {
        tracing::warn!("filter: tried creating prompt for connection without profile");
        None
    })?;

    let target = PromptTarget::from_connection(conn)?;
    let domain = target.domain.clone();
    let ip = target
        .ip
        .unwrap_or(std::net::IpAddr::V4(std::net::Ipv4Addr::UNSPECIFIED));

    // One notification per profile and target.
    let event_id = if conn.inbound || domain.is_empty() {
        format!(
            "{PROMPT_ID_PREFIX}-{}-{}-{ip}",
            profile.id, conn.inbound
        )
    } else {
        format!("{PROMPT_ID_PREFIX}-{}-{domain}", profile.id)
    };

    // Only handle one notification at a time.
    let _guard = PROMPT_NOTIFICATION_CREATION
        .lock()
        .unwrap_or_else(|error| error.into_inner());
    let notifications = env.notifications();

    // If there already is a notification, just update the expiry.
    if let Some(existing) = notifications.get(&event_id) {
        let snapshot = existing.snapshot();
        if snapshot.state == STATE_ACTIVE {
            notifications.update_expiry(&event_id, expires);
            tracing::debug!("filter: updated existing prompt notification");
            return Some(existing);
        }

        // The notification is not active anymore; take the action directly, as
        // the response channel is gone.
        if !snapshot.selected_action_id.is_empty() {
            apply_response(conn, &snapshot.selected_action_id);
            return None;
        }
        // Otherwise create a new notification below.
    }

    let (actions, message) = prompt_actions(conn.inbound, &profile, &target);

    let event_data = json!({
        "Entity": entity_to_json(conn.entity.as_ref().expect("checked above")),
        "Profile": {
            "Source": profile.source,
            "ID": profile.id,
            "LinkedPath": profile.linked_path,
        },
    });

    let mut notification = Notification::prompt(&event_id, "Connection Prompt", &message)
        .with_event_data(event_data)
        .with_expiry(expires);
    notification.category = "Privacy Filter".to_string();
    notification.show_on_system = env.ask_with_system_notifications();
    notification.available_actions = actions;

    let handle = notifications.notify(notification)?;

    // The action handler saves the rule in the local profile.
    let profile_id = profile.id.clone();
    let target_domain = target.domain.clone();
    let target_ip = target.ip;
    handle.set_action_handler(Arc::new(move |notification: &Notification| {
        if notification.selected_action_id == CANCEL_PROMPT {
            return Ok(());
        }
        env.save_response(
            &profile_id,
            &target_domain,
            target_ip,
            &notification.selected_action_id,
        )
    }));

    tracing::debug!("filter: sent prompt notification");
    Some(handle)
}

/// Returns the actions and message of a prompt.
fn prompt_actions(
    inbound: bool,
    profile: &PromptProfile,
    target: &PromptTarget,
) -> (Vec<pm_notifications::Action>, String) {
    let name = &profile.name_or_id();
    if inbound {
        let ip = target.ip.map(|ip| ip.to_string()).unwrap_or_default();
        return (
            vec![
                pm_notifications::Action::new(ALLOW_SERVING_IP, "Allow"),
                pm_notifications::Action::new(BLOCK_SERVING_IP, "Block"),
            ],
            format!(
                "{name} wants to accept connections from {ip} ({}/{})",
                target.protocol, target.port
            ),
        );
    }
    if target.domain.is_empty() {
        let ip = target.ip.map(|ip| ip.to_string()).unwrap_or_default();
        return (
            vec![
                pm_notifications::Action::new(ALLOW_IP, "Allow"),
                pm_notifications::Action::new(BLOCK_IP, "Block"),
            ],
            format!(
                "{name} wants to connect to {ip} ({}/{})",
                target.protocol, target.port
            ),
        );
    }
    (
        vec![
            pm_notifications::Action::new(ALLOW_DOMAIN_ALL, "Allow"),
            pm_notifications::Action::new(BLOCK_DOMAIN_ALL, "Block"),
        ],
        format!("{name} wants to connect to {}", target.domain),
    )
}

impl PromptProfile {
    /// Returns the profile name, falling back to the ID.
    fn name_or_id(&self) -> String {
        if self.name.is_empty() {
            self.id.clone()
        } else {
            self.name.clone()
        }
    }
}

/// Returns the endpoint rule for a prompt answer and whether it is a service
/// (inbound) rule.
///
/// The rule is formatted like Go `Endpoint.String()`: `+ domain`,
/// `+ .domain`, `+ 1.2.3.4` or the same with `- ` for blocks.
pub fn endpoint_rule(
    domain: &str,
    ip: Option<IpAddr>,
    action: &str,
) -> Result<(String, bool), String> {
    let ip = ip
        .map(|ip| ip.to_string())
        .ok_or_else(|| "entity has no IP".to_string())?;

    Ok(match action {
        ALLOW_DOMAIN_ALL => (format!("+ .{domain}"), false),
        ALLOW_DOMAIN_DISTINCT => (format!("+ {domain}"), false),
        BLOCK_DOMAIN_ALL => (format!("- .{domain}"), false),
        BLOCK_DOMAIN_DISTINCT => (format!("- {domain}"), false),
        ALLOW_IP => (format!("+ {ip}"), false),
        ALLOW_SERVING_IP => (format!("+ {ip}"), true),
        BLOCK_IP => (format!("- {ip}"), false),
        BLOCK_SERVING_IP => (format!("- {ip}"), true),
        other => return Err(format!("unknown prompt response: {other}")),
    })
}

/// Returns the current time in Unix seconds.
fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}
