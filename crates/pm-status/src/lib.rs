//! Compatible implementation of the Go `service/status` package.
//!
//! The status module collects state updates of all modules, publishes the
//! overall system status through the runtime database (`runtime:system/status`)
//! and turns module states into notifications.
//!
//! # Compatibility notes
//!
//! * The module manager (`mgr.Manager`) is not part of this crate: callers push
//!   [`StateUpdate`]s into [`Status::handle_module_status_update`] and the
//!   module wires the runtime registry and the state callbacks.
//! * The `WorstState` field embeds `mgr.State` in Go, so its JSON is flattened;
//!   this is reproduced with `#[serde(flatten)]`.
//! * Modules are iterated in sorted order instead of Go's random map order. With
//!   a strictly greater severity comparison, Go may pick either module when two
//!   have equally severe states; here the alphabetically first module wins.
//! * Module states are copied as they are; the notification cleanup removes
//!   notifications of states that disappeared.
//! * The `start` sequence (initial states, callbacks, online status events) lives
//!   in the core; [`Status`] exposes the pieces.

use std::collections::{BTreeMap, HashMap};
use std::sync::{Arc, Mutex, RwLock};

use pm_mgr::{severity, State, StateUpdate};
use pm_netenv::{CaptivePortal, NetEnv, OnlineStatus};
use pm_notifications::{
    Action, Notification, NotificationHandle, Notifications, ACTION_TYPE_OPEN_URL, STATE_ACTIVE,
    TYPE_ERROR, TYPE_INFO, TYPE_WARNING,
};
use pm_record::{Key, Meta, Record};
use pm_runtime::{PushFunc, Registry, Result as RuntimeResult};
use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Event ID of the system status record.
pub const SYSTEM_STATUS_ID: &str = "system/status";
/// Database key of the system status record.
pub const SYSTEM_STATUS_KEY: &str = "runtime:system/status";
/// Link shown by the "Get Help" action of state notifications.
pub const STATE_HELP_URL: &str = "https://safing.io/support/";

/// Security level: off.
pub const SECURITY_LEVEL_OFF: u8 = 0;
/// Security level: normal.
pub const SECURITY_LEVEL_NORMAL: u8 = 1;
/// Security level: high.
pub const SECURITY_LEVEL_HIGH: u8 = 2;
/// Security level: extreme.
pub const SECURITY_LEVEL_EXTREME: u8 = 4;
/// Display hint for security level options.
pub const DISPLAY_HINT_SECURITY_LEVEL: &str = "security level";

/// The most severe state of all modules.
///
/// Go embeds `mgr.State` into an anonymous struct, so the JSON is flattened.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct WorstState {
    #[serde(rename = "Module")]
    pub module: String,
    #[serde(flatten)]
    pub state: State,
}

/// The overall status of the Portmaster, exposed as `runtime:system/status`.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct SystemStatusRecord {
    /// Online status as seen by the network environment.
    #[serde(rename = "OnlineStatus")]
    pub online_status: OnlineStatus,
    /// Captive portal information, if one was detected.
    #[serde(rename = "CaptivePortal")]
    pub captive_portal: Option<CaptivePortal>,
    /// State updates of all modules, sorted by module name.
    #[serde(rename = "Modules")]
    pub modules: Vec<StateUpdate>,
    /// The most severe state of all modules.
    #[serde(rename = "WorstState")]
    pub worst_state: WorstState,
}

/// The status module.
pub struct Status {
    netenv: Arc<NetEnv>,
    notifications: Arc<Notifications>,
    states: Mutex<BTreeMap<String, StateUpdate>>,
    module_notifications: Mutex<HashMap<String, BTreeMap<String, Arc<NotificationHandle>>>>,
    push_update: RwLock<Option<PushFunc>>,
}

/// Errors of the status module.
pub type Error = pm_runtime::Error;

impl Status {
    /// Creates a status module for the given network environment and
    /// notification registry.
    pub fn new(netenv: Arc<NetEnv>, notifications: Arc<Notifications>) -> Status {
        Status {
            netenv,
            notifications,
            states: Mutex::new(BTreeMap::new()),
            module_notifications: Mutex::new(HashMap::new()),
            push_update: RwLock::new(None),
        }
    }

    /// Registers the system status provider at `system/status`, like Go
    /// `setupRuntimeProvider`.
    ///
    /// The provider computes a fresh record on every read, so subscribers always
    /// see the current state.
    pub fn register(self: &Arc<Self>, registry: &Registry) -> RuntimeResult<()> {
        let weak = Arc::downgrade(self);
        let provider = Arc::new(pm_runtime::SimpleValueGetter::new(move |_key| {
            let Some(status) = weak.upgrade() else {
                return Ok(Vec::new());
            };
            Ok(vec![status.system_status_record()])
        }));
        let push = registry.register(SYSTEM_STATUS_ID, provider)?;
        *self.push_update.write().expect("lock poisoned") = Some(push);
        Ok(())
    }

    /// Returns the network environment of this module.
    pub fn netenv(&self) -> &Arc<NetEnv> {
        &self.netenv
    }

    /// Returns the notification registry of this module.
    pub fn notifications(&self) -> &Arc<Notifications> {
        &self.notifications
    }

    /// Returns the last known state update of every module, sorted by name.
    pub fn states(&self) -> Vec<StateUpdate> {
        self.states
            .lock()
            .expect("lock poisoned")
            .values()
            .cloned()
            .collect()
    }

    /// Applies a module state update, like Go `handleModuleStatusUpdate`.
    ///
    /// Stores the update, derives notifications from it and publishes a new
    /// system status.
    pub fn handle_module_status_update(&self, update: StateUpdate) {
        self.states
            .lock()
            .expect("lock poisoned")
            .insert(update.module.clone(), update.clone());
        self.derive_notifications_from_state_update(&update);
        self.publish_system_status();
    }

    /// Builds the current system status record.
    pub fn system_status(&self) -> SystemStatusRecord {
        let modules: Vec<StateUpdate> = self
            .states
            .lock()
            .expect("lock poisoned")
            .values()
            .cloned()
            .collect();

        let mut worst_state = WorstState::default();
        for update in &modules {
            for state in &update.states {
                if severity(&state.type_) > severity(&worst_state.state.type_) {
                    worst_state.module = update.module.clone();
                    worst_state.state = state.clone();
                }
            }
        }

        SystemStatusRecord {
            online_status: self.netenv.online_status(),
            captive_portal: self.netenv.captive_portal(),
            modules,
            worst_state,
        }
    }

    /// Builds the current system status as a database record with a fresh meta.
    pub fn system_status_record(&self) -> Record {
        let status = self.system_status();
        let data = pm_dsd::gojson::to_vec(&status).unwrap_or_default();
        let now = now();
        Record {
            key: Key::new("runtime", SYSTEM_STATUS_ID),
            meta: Meta {
                created: now,
                modified: now,
                ..Meta::default()
            },
            format: pm_dsd::format::JSON,
            data,
        }
    }

    /// Pushes a new system status to the runtime database subscribers, like Go
    /// `publishSystemStatus`.
    pub fn publish_system_status(&self) {
        let push = self.push_update.read().expect("lock poisoned").clone();
        let Some(push) = push else {
            return;
        };
        push(&[self.system_status_record()]);
    }

    /// Creates and deletes notifications for the states of an update, like Go
    /// `deriveNotificationsFromStateUpdate`.
    pub fn derive_notifications_from_state_update(&self, update: &StateUpdate) {
        let mut module_notifications = self.module_notifications.lock().expect("lock poisoned");
        let notifications = module_notifications
            .entry(update.module.clone())
            .or_default();

        let mut seen = Vec::with_capacity(update.states.len());
        for state in &update.states {
            seen.push(state.id.clone());

            // A notification for this state already exists.
            if notifications.contains_key(&state.id) {
                continue;
            }

            // Reuse a notification that was created beforehand.
            if let Some(handle) = self.notifications.get(&state.id) {
                notifications.insert(state.id.clone(), handle);
                continue;
            }

            let (type_, show_on_system, actions) = match state.type_.as_str() {
                pm_mgr::STATE_TYPE_WARNING => (
                    TYPE_WARNING,
                    true,
                    vec![Action::new("", "Get Help").with_type(
                        ACTION_TYPE_OPEN_URL,
                        Value::String(STATE_HELP_URL.to_string()),
                    )],
                ),
                pm_mgr::STATE_TYPE_ERROR => (
                    TYPE_ERROR,
                    true,
                    vec![Action::new("", "Get Help").with_type(
                        ACTION_TYPE_OPEN_URL,
                        Value::String(STATE_HELP_URL.to_string()),
                    )],
                ),
                // Hints and undefined states are informational and have no
                // call to action.
                _ => (TYPE_INFO, false, Vec::new()),
            };

            let notification = Notification {
                event_id: state.id.clone(),
                type_,
                title: state.name.clone(),
                message: state.message.clone(),
                show_on_system,
                available_actions: actions,
                state: STATE_ACTIVE.to_string(),
                ..Notification::default()
            };

            if let Some(handle) = self.notifications.notify(notification) {
                notifications.insert(state.id.clone(), handle);
            }
        }

        // Remove notifications of states that disappeared.
        notifications.retain(|state_id, handle| {
            let keep = seen.contains(state_id);
            if !keep {
                self.notifications.delete_handle(handle, true);
            }
            keep
        });
    }
}

/// Returns the current time in Unix seconds.
fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}

/// Migrates a security level (integer) option value to a boolean, like Go
/// `MigrateSecurityLevelToBoolean`.
///
/// Values that are not numeric, or options that are not boolean, are returned
/// unchanged.
pub fn migrate_security_level_to_boolean(
    option_type: pm_config::OptionType,
    value: &Value,
) -> Value {
    if option_type != pm_config::OPT_TYPE_BOOL {
        return value.clone();
    }

    let number = match value {
        Value::Number(number) => {
            if let Some(value) = number.as_u64() {
                value as u8
            } else if let Some(value) = number.as_i64() {
                (value as i128 & 0xff) as u8
            } else if let Some(value) = number.as_f64() {
                (value as i64 as i128 & 0xff) as u8
            } else {
                return value.clone();
            }
        }
        _ => return value.clone(),
    };

    Value::Bool(number & SECURITY_LEVEL_NORMAL > 0)
}
