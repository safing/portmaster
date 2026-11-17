//! Port of Go `service/control`: pause and resume the core with a timed
//! automatic resume. The SPN is represented by `spn/enable`, but the SPN
//! service itself is not part of the Rust core, so the environment only needs
//! to report whether it is stopped.
//!
//! Timers use wall-clock time like Go (monotonic time would break across VM
//! suspend), and the resume worker also re-checks the deadline every 15s.

use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use async_trait::async_trait;
use pm_notifications::{Action, ActionTypeWebhookPayload, Notification, ACTION_TYPE_WEBHOOK, TYPE_ERROR, TYPE_INFO, TYPE_WARNING};
use serde::{Deserialize, Serialize};
use tokio::sync::{watch, Mutex};
use tokio::task::JoinHandle;

/// API path of the pause action.
pub const API_ENDPOINT_PAUSE: &str = "control/pause";
/// API path of the resume action.
pub const API_ENDPOINT_RESUME: &str = "control/resume";

const PAUSED_EVENT_ID: &str = "control:paused";
const RESUMED_EVENT_ID: &str = "control:resumed";
const ERROR_EVENT_ID: &str = "control:error";
const STATUS_EVENT_ID: &str = "control:status_info";
const SPN_STOP_TIMEOUT: Duration = Duration::from_secs(30);
const CLOCK_CHECK_INTERVAL: Duration = Duration::from_secs(15);

/// Errors returned by the control module.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("{0}")]
    Message(String),
}

/// Result type of the control module.
pub type Result<T> = std::result::Result<T, Error>;

fn error(message: impl Into<String>) -> Error {
    Error::Message(message.into())
}

/// Go `PauseInfo`, including its field names in the notification event data.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct PauseInfo {
    #[serde(rename = "Interception")]
    pub interception: bool,
    #[serde(rename = "SPN")]
    pub spn: bool,
    #[serde(rename = "TillTime", serialize_with = "serialize_time", deserialize_with = "deserialize_time")]
    pub till_time: i64,
}

fn serialize_time<S: serde::Serializer>(seconds: &i64, serializer: S) -> std::result::Result<S::Ok, S::Error>
where
    S::Error: serde::ser::Error,
{
    let text = if *seconds == 0 {
        "0001-01-01T00:00:00Z".to_string()
    } else {
        pm_dsd::gotime::format_rfc3339(*seconds)
    };
    serializer.serialize_str(&text)
}

fn deserialize_time<'de, D: serde::Deserializer<'de>>(deserializer: D) -> std::result::Result<i64, D::Error>
where
    D::Error: serde::de::Error,
{
    let text = String::deserialize(deserializer)?;
    Ok(pm_dsd::gotime::parse_rfc3339(&text).unwrap_or(0))
}

/// Format a Unix timestamp the way Go's `time.TimeOnly` does.
fn time_only(seconds: i64) -> String {
    if seconds == 0 {
        return "00:00:00".to_string();
    }
    let text = pm_dsd::gotime::format_rfc3339(seconds);
    text.get(11..19).unwrap_or("00:00:00").to_string()
}

/// Everything the control module needs from the core.
#[async_trait]
pub trait ControlEnv: Send + Sync + 'static {
    /// Whether the core is shutting down.
    fn is_shutting_down(&self) -> bool;
    /// Value of the `spn/enable` option.
    fn spn_enabled(&self) -> bool;
    /// Writes the `spn/enable` option.
    fn set_spn_enabled(&self, enabled: bool);
    /// Whether the SPN service has stopped. With no SPN in the process this is
    /// always true.
    fn spn_is_stopped(&self) -> bool;
    /// Stops packet interception.
    async fn stop_interception(&self) -> Result<()>;
    /// Starts packet interception.
    async fn start_interception(&self) -> Result<()>;
}

/// Notification sink implemented by the core over `pm_notifications`.
pub trait NotificationSink: Send + Sync {
    fn notify(&self, notification: Notification);
    fn delete(&self, event_id: &str);
}

struct State {
    info: PauseInfo,
    resume_worker: Option<JoinHandle<()>>,
    pause_notification: Option<Notification>,
}

/// The pause/resume state machine.
pub struct Control<E: ControlEnv> {
    env: E,
    notifications: Arc<dyn NotificationSink>,
    state: Mutex<State>,
    spn_enabled: watch::Sender<bool>,
}

impl<E: ControlEnv> Control<E> {
    /// Creates a control module with a notification sink.
    pub fn new(env: E, notifications: Arc<dyn NotificationSink>) -> Control<E> {
        let (spn_enabled, _) = watch::channel(env.spn_enabled());
        Control {
            env,
            notifications,
            state: Mutex::new(State {
                info: PauseInfo::default(),
                resume_worker: None,
                pause_notification: None,
            }),
            spn_enabled,
        }
    }

    /// Records an external change of `spn/enable` so a running resume worker
    /// can auto-resume, like Go's config-change subscription.
    pub fn observe_spn_enabled(&self, enabled: bool) {
        let _ = self.spn_enabled.send(enabled);
    }

    /// Current pause info, as exposed in the notification event data.
    pub async fn info(&self) -> PauseInfo {
        self.state.lock().await.info.clone()
    }

    /// Stops any pending resume worker, like Go `Control.Stop`.
    pub async fn shutdown(&self) {
        let mut state = self.state.lock().await;
        stop_resume_worker(&mut state);
    }

    /// Pauses the core (or only the SPN) for the given duration.
    pub async fn pause(self: &Arc<Self>, duration: Duration, only_spn: bool) -> Result<()> {
        if self.env.is_shutting_down() {
            return Ok(());
        }

        let mut state = self.state.lock().await;
        let outcome = self.pause_locked(&mut state, duration, only_spn).await;
        // Like Go's deferred cleanup: always refresh the pause notification,
        // but a failed pause is only logged - it does not raise control:error.
        self.update_states_and_notify(&mut state);
        outcome
    }

    async fn pause_locked(
        self: &Arc<Self>,
        state: &mut State,
        duration: Duration,
        only_spn: bool,
    ) -> Result<()> {
        if duration.is_zero() {
            return Err(error("invalid pause duration"));
        }

        let spn_enabled = self.env.spn_enabled();
        if only_spn {
            if state.info.interception {
                return Err(error("cannot pause SPN separately when core is paused"));
            }
            if !spn_enabled && !state.info.spn {
                return Err(error("cannot pause SPN when it is not running"));
            }
        }

        // A new pause replaces the old timer.
        stop_resume_worker(state);

        if !state.info.spn && spn_enabled {
            self.env.set_spn_enabled(false);
            self.observe_spn_enabled(false);
            if let Err(error) = self.wait_spn_stopped().await {
                self.env.set_spn_enabled(true);
                self.observe_spn_enabled(true);
                return Err(error);
            }
            state.info.spn = true;
        }

        if only_spn {
            start_resume_worker(self, state, duration);
            return Ok(());
        }

        if !state.info.interception {
            self.env.stop_interception().await?;
            state.info.interception = true;
        }

        start_resume_worker(self, state, duration);
        Ok(())
    }

    /// Resumes normal operation.
    pub async fn resume(self: &Arc<Self>) -> Result<()> {
        if self.env.is_shutting_down() {
            return Ok(());
        }

        let mut state = self.state.lock().await;
        stop_resume_worker(&mut state);
        let outcome = self.resume_locked(&mut state).await;
        match &outcome {
            Ok(()) => self.update_states_and_notify(&mut state),
            // Go calls updateStatesAndNotifyError on resume failure instead of
            // the normal state notification.
            Err(error) => {
                self.update_states_and_notify_error(&mut state, "Resume operation failed", error)
            }
        }
        outcome
    }

    async fn resume_locked(&self, state: &mut State) -> Result<()> {
        if state.info.interception {
            self.env.start_interception().await?;
            state.info.interception = false;
        }
        if state.info.spn {
            if !self.env.spn_enabled() {
                self.env.set_spn_enabled(true);
                self.observe_spn_enabled(true);
            }
            state.info.spn = false;
        }
        Ok(())
    }

    async fn wait_spn_stopped(&self) -> Result<()> {
        let started = Instant::now();
        let mut waiting_notification = false;
        while !self.env.spn_is_stopped() {
            tokio::time::sleep(Duration::from_millis(200)).await;
            if self.env.is_shutting_down() {
                return Err(error("shutting down"));
            }
            if started.elapsed() > SPN_STOP_TIMEOUT {
                return Err(error("timeout waiting for SPN to stop"));
            }
            if !waiting_notification && started.elapsed() > Duration::from_secs(1) {
                self.notifications
                    .notify(Notification::info(STATUS_EVENT_ID, "Waiting for SPN to stop...", ""));
                waiting_notification = true;
            }
            if self.env.spn_enabled() {
                return Err(error("SPN enabled again"));
            }
        }
        if waiting_notification {
            self.notifications.delete(STATUS_EVENT_ID);
        }
        Ok(())
    }

    fn update_states_and_notify(&self, state: &mut State) {
        if !state.info.interception && !state.info.spn {
            if state.pause_notification.take().is_some() {
                self.notifications.delete(PAUSED_EVENT_ID);
            }
            return;
        }

        let (title, type_) = if state.info.interception && state.info.spn {
            ("Portmaster and SPN paused", TYPE_WARNING)
        } else if state.info.interception {
            ("Portmaster paused", TYPE_WARNING)
        } else {
            ("SPN paused", TYPE_INFO)
        };
        let message = format!("{} until {}", title, time_only(state.info.till_time));

        let mut notification = Notification {
            event_id: PAUSED_EVENT_ID.to_string(),
            type_,
            title: title.to_string(),
            message,
            show_on_system: false,
            event_data: serde_json::to_value(&state.info).unwrap_or_default(),
            ..Notification::default()
        };
        notification.available_actions.push(Action {
            text: "Resume".to_string(),
            type_: ACTION_TYPE_WEBHOOK.to_string(),
            payload: serde_json::to_value(ActionTypeWebhookPayload {
                url: API_ENDPOINT_RESUME.to_string(),
                result_action: "display".to_string(),
                ..ActionTypeWebhookPayload::default()
            })
            .unwrap_or_default(),
            ..Action::default()
        });

        self.notifications.notify(notification.clone());
        state.pause_notification = Some(notification);
    }

    /// Mirrors Go `updateStatesAndNotifyError`: raise a `control:error`
    /// notification carrying the current pause info, shown only in the app.
    /// No normal state notification is sent on this path.
    fn update_states_and_notify_error(
        &self,
        state: &mut State,
        err_description: &str,
        error: &Error,
    ) {
        let title = if err_description.is_empty() {
            "Error"
        } else {
            err_description
        };
        let notification = Notification {
            event_id: ERROR_EVENT_ID.to_string(),
            type_: TYPE_ERROR,
            title: title.to_string(),
            message: error.to_string(),
            show_on_system: false,
            event_data: serde_json::to_value(&state.info).unwrap_or_default(),
            ..Notification::default()
        };
        self.notifications.notify(notification.clone());
        state.pause_notification = Some(notification);
    }
}

fn stop_resume_worker(state: &mut State) {
    state.info.till_time = 0;
    if let Some(worker) = state.resume_worker.take() {
        worker.abort();
    }
}

fn start_resume_worker<E: ControlEnv>(
    control: &Arc<Control<E>>,
    state: &mut State,
    duration: Duration,
) {
    // Strip monotonic time, like Go's `time.Now().Round(0)`: wall-clock changes
    // must be detected by the periodic check below.
    let deadline = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .checked_add(duration)
        .unwrap_or_default();
    state.info.till_time = deadline.as_secs() as i64;

    let mut spn_changes = control.spn_enabled.subscribe();
    let control = Arc::clone(control);
    let worker = tokio::spawn(async move {
        let mut ticker = tokio::time::interval(CLOCK_CHECK_INTERVAL);
        ticker.tick().await; // first tick is immediate
        loop {
            let remaining = deadline
                .checked_sub(
                    SystemTime::now()
                        .duration_since(UNIX_EPOCH)
                        .unwrap_or_default(),
                )
                .unwrap_or_default();
            tokio::select! {
                _ = tokio::time::sleep(remaining) => {}
                changed = spn_changes.changed() => {
                    if changed.is_err() || !*spn_changes.borrow() {
                        continue;
                    }
                }
                _ = ticker.tick() => {
                    if SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default() < deadline {
                        continue;
                    }
                }
            }

            if let Err(error) = control.resume().await {
                tracing::warn!(%error, "control: automatic resume failed");
                return;
            }
            let mut notification = Notification::info(
                RESUMED_EVENT_ID,
                "Resumed",
                "Automatically resumed from pause state",
            );
            notification.show_on_system = true;
            notification.expires = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap_or_default()
                .as_secs() as i64
                + 15;
            notification.available_actions.push(Action::new("ack", "OK"));
            control.notifications.notify(notification);
            return;
        }
    });
    state.resume_worker = Some(worker);
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

    #[derive(Clone)]
    struct MockEnv {
        shutting_down: Arc<AtomicBool>,
        spn_enabled: Arc<AtomicBool>,
        spn_stopped: Arc<AtomicBool>,
        interception_running: Arc<AtomicBool>,
        stop_calls: Arc<AtomicUsize>,
        start_calls: Arc<AtomicUsize>,
        start_fails: Arc<AtomicBool>,
    }

    impl MockEnv {
        fn new() -> MockEnv {
            MockEnv {
                shutting_down: Arc::new(AtomicBool::new(false)),
                spn_enabled: Arc::new(AtomicBool::new(false)),
                spn_stopped: Arc::new(AtomicBool::new(true)),
                interception_running: Arc::new(AtomicBool::new(true)),
                stop_calls: Arc::new(AtomicUsize::new(0)),
                start_calls: Arc::new(AtomicUsize::new(0)),
                start_fails: Arc::new(AtomicBool::new(false)),
            }
        }
    }

    #[async_trait]
    impl ControlEnv for MockEnv {
        fn is_shutting_down(&self) -> bool {
            self.shutting_down.load(Ordering::SeqCst)
        }
        fn spn_enabled(&self) -> bool {
            self.spn_enabled.load(Ordering::SeqCst)
        }
        fn set_spn_enabled(&self, enabled: bool) {
            self.spn_enabled.store(enabled, Ordering::SeqCst);
        }
        fn spn_is_stopped(&self) -> bool {
            self.spn_stopped.load(Ordering::SeqCst)
        }
        async fn stop_interception(&self) -> Result<()> {
            self.stop_calls.fetch_add(1, Ordering::SeqCst);
            self.interception_running.store(false, Ordering::SeqCst);
            Ok(())
        }
        async fn start_interception(&self) -> Result<()> {
            if self.start_fails.load(Ordering::SeqCst) {
                return Err(error("cannot start interception"));
            }
            self.start_calls.fetch_add(1, Ordering::SeqCst);
            self.interception_running.store(true, Ordering::SeqCst);
            Ok(())
        }
    }

    #[derive(Default)]
    struct MockNotifications {
        notified: std::sync::Mutex<Vec<Notification>>,
        deleted: std::sync::Mutex<Vec<String>>,
    }

    impl NotificationSink for MockNotifications {
        fn notify(&self, notification: Notification) {
            self.notified.lock().unwrap().push(notification);
        }
        fn delete(&self, event_id: &str) {
            self.deleted.lock().unwrap().push(event_id.to_string());
        }
    }

    fn setup() -> (Arc<Control<MockEnv>>, MockEnv, Arc<MockNotifications>) {
        let env = MockEnv::new();
        let notifications = Arc::new(MockNotifications::default());
        let control = Arc::new(Control::new(
            env.clone(),
            notifications.clone() as Arc<dyn NotificationSink>,
        ));
        (control, env, notifications)
    }

    #[tokio::test]
    async fn rejects_invalid_duration() {
        let (control, _, _) = setup();
        assert_eq!(
            control.pause(Duration::ZERO, false).await.unwrap_err().to_string(),
            "invalid pause duration"
        );
    }

    #[tokio::test]
    async fn rejects_spn_only_pause_when_spn_is_off() {
        let (control, _, _) = setup();
        assert_eq!(
            control
                .pause(Duration::from_secs(60), true)
                .await
                .unwrap_err()
                .to_string(),
            "cannot pause SPN when it is not running"
        );
    }

    #[tokio::test]
    async fn pauses_and_resumes_interception() {
        let (control, _, notifications) = setup();
        control.pause(Duration::from_secs(60), false).await.unwrap();
        let info = control.info().await;
        assert!(info.interception);
        assert!(!info.spn);
        assert_ne!(info.till_time, 0);

        let paused = notifications
            .notified
            .lock()
            .unwrap()
            .iter()
            .find(|n| n.event_id == PAUSED_EVENT_ID)
            .cloned()
            .unwrap();
        assert_eq!(paused.title, "Portmaster paused");
        assert!(paused.message.contains(" until "));
        assert_eq!(paused.available_actions[0].text, "Resume");
        assert_eq!(paused.available_actions[0].type_, ACTION_TYPE_WEBHOOK);
        let data: PauseInfo = serde_json::from_value(paused.event_data).unwrap();
        assert!(data.interception);
        assert_eq!(data.till_time, info.till_time);

        control.resume().await.unwrap();
        let info = control.info().await;
        assert!(!info.interception && !info.spn);
        assert_eq!(info.till_time, 0);
        assert!(notifications.deleted.lock().unwrap().contains(&PAUSED_EVENT_ID.to_string()));
    }

    #[tokio::test]
    async fn pauses_spn_when_enabled_and_restores_config() {
        let (control, env, _) = setup();
        env.spn_enabled.store(true, Ordering::SeqCst);

        control.pause(Duration::from_secs(60), true).await.unwrap();
        let info = control.info().await;
        assert!(info.spn);
        assert!(!info.interception);
        assert!(!env.spn_enabled.load(Ordering::SeqCst));

        control.resume().await.unwrap();
        assert!(env.spn_enabled.load(Ordering::SeqCst));
        assert!(!control.info().await.spn);
    }

    #[tokio::test]
    async fn resumes_automatically_after_deadline() {
        let (control, _, notifications) = setup();
        control
            .pause(Duration::from_millis(50), false)
            .await
            .unwrap();
        tokio::time::sleep(Duration::from_millis(200)).await;
        assert!(!control.info().await.interception);
        assert!(notifications
            .notified
            .lock()
            .unwrap()
            .iter()
            .any(|n| n.event_id == RESUMED_EVENT_ID));
    }

    #[tokio::test]
    async fn resumes_when_spn_is_enabled_by_user() {
        let (control, _, _) = setup();
        control.pause(Duration::from_secs(60), false).await.unwrap();
        control.observe_spn_enabled(true);
        tokio::time::sleep(Duration::from_millis(100)).await;
        assert!(!control.info().await.interception);
    }

    #[test]
    fn pause_info_uses_go_field_names_and_zero_time() {
        let json = serde_json::to_string(&PauseInfo::default()).unwrap();
        assert_eq!(
            json,
            "{\"Interception\":false,\"SPN\":false,\"TillTime\":\"0001-01-01T00:00:00Z\"}"
        );
    }

    #[tokio::test]
    async fn pause_error_does_not_raise_error_notification() {
        let (control, _, notifications) = setup();
        assert!(control.pause(Duration::ZERO, false).await.is_err());
        assert!(notifications
            .notified
            .lock()
            .unwrap()
            .iter()
            .all(|n| n.event_id != ERROR_EVENT_ID));
    }

    #[tokio::test]
    async fn resume_error_notifies_control_error_with_pause_info() {
        let (control, env, notifications) = setup();
        control.pause(Duration::from_secs(60), false).await.unwrap();
        env.start_fails.store(true, Ordering::SeqCst);
        assert!(control.resume().await.is_err());

        let notification = notifications
            .notified
            .lock()
            .unwrap()
            .iter()
            .find(|n| n.event_id == ERROR_EVENT_ID)
            .cloned()
            .expect("control:error notification");
        assert_eq!(notification.type_, TYPE_ERROR);
        assert_eq!(notification.title, "Resume operation failed");
        assert!(!notification.show_on_system);
        let data: PauseInfo = serde_json::from_value(notification.event_data).unwrap();
        assert!(data.interception);
        // The normal state path must not run on error, so the paused
        // notification is not deleted.
        assert!(notifications.deleted.lock().unwrap().is_empty());
    }
}
