//! Broadcast handling, ported from Go `service/broadcasts/notify.go`.

use std::fmt;
use std::sync::Arc;

use pm_notifications::{Action, Notifications};
use pm_record::accessor::JsonAccessor;

use crate::source::{BroadcastNotification, BROADCAST_NOTIFICATION_ID_PREFIX};
use crate::state::BroadcastStates;
use crate::ModuleAttachment;

/// Reasons a broadcast was skipped, matching Go's `ErrSkip` family exactly.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SkipReason {
    DoesNotMatch,
    AlreadyActive,
    AlreadyShown,
    RemovedByMismatch,
    RemovedBySource,
}

impl fmt::Display for SkipReason {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let message = match self {
            SkipReason::DoesNotMatch => "broadcast skipped: does not match",
            SkipReason::AlreadyActive => "broadcast skipped: already active",
            SkipReason::AlreadyShown => "broadcast skipped: already shown",
            SkipReason::RemovedByMismatch => "broadcast skipped: removed due to mismatch",
            SkipReason::RemovedBySource => "broadcast skipped: removed by source",
        };
        f.write_str(message)
    }
}

/// Outcome of handling one broadcast.
#[derive(Debug)]
pub enum HandleResult {
    Displayed,
    Skipped(SkipReason),
    Failed(String),
}

impl fmt::Display for HandleResult {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            HandleResult::Displayed => f.write_str("displayed"),
            HandleResult::Skipped(reason) => reason.fmt(f),
            HandleResult::Failed(error) => f.write_str(error),
        }
    }
}

/// Removes any active notification and its state, like Go `removeBroadcast`.
pub(crate) fn remove_broadcast(
    id: &str,
    bss: Option<&mut BroadcastStates>,
    notifications: &Notifications,
) -> bool {
    let event_id = format!("{BROADCAST_NOTIFICATION_ID_PREFIX}{id}");
    let mut removed = false;
    if notifications.get(&event_id).is_some() {
        removed = true;
        notifications.delete(&event_id);
    }
    if let Some(bss) = bss {
        bss.states.remove(id);
    }
    removed
}

/// Handles a single broadcast, like Go `handleBroadcast`.
///
/// `on_ack` is the action callback installed on the notification; the core
/// records the acknowledgement in the database when the user selects `ack`.
pub(crate) fn handle_broadcast<F>(
    broadcast: &BroadcastNotification,
    matching: &JsonAccessor,
    mut bss: Option<&mut BroadcastStates>,
    notifications: &Notifications,
    attachment: &dyn ModuleAttachment,
    on_ack: F,
    now: i64,
) -> HandleResult
where
    F: Fn(String, String) -> std::result::Result<(), String> + Send + Sync + 'static,
{
    // Check whether the broadcast was already shown.
    if let Some(bss) = bss.as_deref_mut() {
        let already_shown = match bss.states.get(&broadcast.id) {
            Some(state) if state.read > 0 => {
                broadcast.repeat_duration.is_zero()
                    || now < state.read + i64::try_from(broadcast.repeat_duration.as_secs()).unwrap_or(i64::MAX)
            }
            _ => false,
        };
        if already_shown {
            return HandleResult::Skipped(SkipReason::AlreadyShown);
        }
    }

    // Check whether the source asked us to remove this broadcast.
    if broadcast.remove {
        remove_broadcast(&broadcast.id, bss, notifications);
        return HandleResult::Skipped(SkipReason::RemovedBySource);
    }

    // Check whether the broadcast matches the local data.
    if let Some(query) = &broadcast.matching_query {
        let matches = match query.where_condition() {
            Some(condition) => condition.complies(matching),
            None => true,
        };
        if !matches {
            let removed = remove_broadcast(&broadcast.id, bss, notifications);
            return HandleResult::Skipped(if removed {
                SkipReason::RemovedByMismatch
            } else {
                SkipReason::DoesNotMatch
            });
        }
    }

    let event_id = format!("{BROADCAST_NOTIFICATION_ID_PREFIX}{}", broadcast.id);
    if notifications.get(&event_id).is_some() {
        return HandleResult::Skipped(SkipReason::AlreadyActive);
    }

    // Prepare the notification for display. The notification is recreated from
    // the YAML on every run, so editing it here is safe, just like in Go.
    let mut notification = broadcast.notification.clone();
    notification.event_id = event_id.clone();
    notification.guid.clear();
    notification.state.clear();
    notification.selected_action_id.clear();
    if !broadcast.permanent {
        notification
            .available_actions
            .push(Action::new("ack", "Got it!"));
    }

    let Some(handle) = notifications.save(notification, true) else {
        return HandleResult::Failed("failed to save notification".to_string());
    };
    handle.set_action_handler(Arc::new(move |notification| {
        let prefix = BROADCAST_NOTIFICATION_ID_PREFIX;
        let broadcast_id = notification
            .event_id
            .strip_prefix(prefix)
            .unwrap_or(&notification.event_id)
            .to_string();
        on_ack(broadcast_id, notification.selected_action_id.clone())
    }));

    if broadcast.attach_to_module {
        attachment.attach(&event_id);
    }

    HandleResult::Displayed
}
