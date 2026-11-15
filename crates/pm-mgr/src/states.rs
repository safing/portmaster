//! Module and manager states, a port of Go `service/mgr/states.go`.

use std::sync::Mutex;

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::events::EventMgr;

/// Returns the current time in Unix seconds.
fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs() as i64)
        .unwrap_or(0)
}

/// Type of a state.
pub type StateType = String;

/// Undefined state type.
pub const STATE_TYPE_UNDEFINED: &str = "";
/// Hint state type.
pub const STATE_TYPE_HINT: &str = "hint";
/// Warning state type.
pub const STATE_TYPE_WARNING: &str = "warning";
/// Error state type.
pub const STATE_TYPE_ERROR: &str = "error";

/// Returns a number representing the gravity of the state, like Go
/// `StateType.Severity`.
pub fn severity(state_type: &str) -> u8 {
    match state_type {
        STATE_TYPE_HINT => 1,
        STATE_TYPE_WARNING => 2,
        STATE_TYPE_ERROR => 3,
        _ => 0,
    }
}

/// Describes the state of a manager or module.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct State {
    /// Program-unique ID of the state.
    #[serde(rename = "ID")]
    pub id: String,
    /// Name of the state; may be used as a notification title.
    #[serde(rename = "Name")]
    pub name: String,
    /// More detailed message about the state.
    #[serde(rename = "Message")]
    pub message: String,
    /// Type of the state.
    #[serde(rename = "Type")]
    pub type_: StateType,
    /// Time of the state in Unix seconds; rendered like Go's `time.Time`.
    #[serde(
        rename = "Time",
        serialize_with = "serialize_time",
        deserialize_with = "deserialize_time",
        default
    )]
    pub time: i64,
    /// Additional data for connected systems.
    #[serde(rename = "Data")]
    pub data: Value,
}

fn serialize_time<S: serde::Serializer>(seconds: &i64, serializer: S) -> Result<S::Ok, S::Error> {
    serializer.serialize_str(&pm_dsd::gotime::format_rfc3339(*seconds))
}

fn deserialize_time<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<i64, D::Error> {
    let text = String::deserialize(deserializer)?;
    Ok(pm_dsd::gotime::parse_rfc3339(&text).unwrap_or(0))
}

impl State {
    /// Creates a state with the given ID, name and type.
    pub fn new(id: &str, name: &str, type_: &str) -> State {
        State {
            id: id.to_string(),
            name: name.to_string(),
            type_: type_.to_string(),
            ..State::default()
        }
    }

    /// Sets the message.
    #[must_use]
    pub fn with_message(mut self, message: &str) -> State {
        self.message = message.to_string();
        self
    }

    /// Sets the time of the state.
    #[must_use]
    pub fn with_time(mut self, time: i64) -> State {
        self.time = time;
        self
    }

    /// Sets the additional data.
    #[must_use]
    pub fn with_data(mut self, data: Value) -> State {
        self.data = data;
        self
    }

    /// Returns the severity of the state type.
    pub fn severity(&self) -> u8 {
        severity(&self.type_)
    }
}

/// Updates others about a state change.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct StateUpdate {
    #[serde(rename = "Module")]
    pub module: String,
    #[serde(rename = "States")]
    pub states: Vec<State>,
}

impl StateUpdate {
    /// Creates an update for the given module.
    pub fn new(module: &str, states: Vec<State>) -> StateUpdate {
        StateUpdate {
            module: module.to_string(),
            states,
        }
    }
}

/// A simple state manager, like Go `StateMgr`.
pub struct StateMgr {
    module_name: String,
    states: Mutex<Vec<State>>,
    events: EventMgr<StateUpdate>,
}

impl StateMgr {
    /// Creates a state manager for the given module.
    pub fn new(module_name: &str) -> StateMgr {
        StateMgr {
            module_name: module_name.to_string(),
            states: Mutex::new(Vec::new()),
            events: EventMgr::new("state update"),
        }
    }

    /// Returns the event manager of state updates.
    pub fn events(&self) -> &EventMgr<StateUpdate> {
        &self.events
    }

    /// Adds a state; a state with the same ID is replaced.
    pub fn add(&self, mut state: State) {
        if state.time == 0 {
            state.time = now();
        }

        {
            let mut states = self.states.lock().expect("lock poisoned");
            match states.iter_mut().find(|existing| existing.id == state.id) {
                Some(existing) => *existing = state,
                None => states.push(state),
            }
        }
        self.events.submit(self.export());
    }

    /// Removes the state with the given ID.
    pub fn remove(&self, id: &str) {
        let removed = {
            let mut states = self.states.lock().expect("lock poisoned");
            if states.is_empty() {
                return;
            }
            let before = states.len();
            states.retain(|state| state.id != id);
            states.len() != before
        };

        if removed {
            self.events.submit(self.export());
        }
    }

    /// Removes all states.
    pub fn clear(&self) {
        {
            let mut states = self.states.lock().expect("lock poisoned");
            states.clear();
        }
        // Submit without the lock; callbacks may change the states again.
        self.events.submit(self.export());
    }

    /// Returns the current states as an update.
    pub fn export(&self) -> StateUpdate {
        StateUpdate {
            module: self.module_name.clone(),
            states: self.states.lock().expect("lock poisoned").clone(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn severities_match_go() {
        assert_eq!(severity(STATE_TYPE_UNDEFINED), 0);
        assert_eq!(severity(STATE_TYPE_HINT), 1);
        assert_eq!(severity(STATE_TYPE_WARNING), 2);
        assert_eq!(severity(STATE_TYPE_ERROR), 3);
        assert_eq!(severity("unknown"), 0);
    }

    #[test]
    fn states_are_added_replaced_and_removed() {
        let manager = StateMgr::new("module");
        let mut updates = manager.events().subscribe("test", 8);

        manager.add(State::new("id-1", "First", STATE_TYPE_HINT));
        assert_eq!(manager.export().states.len(), 1);
        assert!(manager.export().states[0].time > 0, "time is filled in");

        // Replacing keeps the order and does not duplicate.
        manager.add(State::new("id-1", "Replaced", STATE_TYPE_WARNING));
        let export = manager.export();
        assert_eq!(export.states.len(), 1);
        assert_eq!(export.states[0].name, "Replaced");

        manager.add(State::new("id-2", "Second", STATE_TYPE_ERROR));
        assert_eq!(manager.export().states.len(), 2);

        manager.remove("id-1");
        let export = manager.export();
        assert_eq!(export.states.len(), 1);
        assert_eq!(export.states[0].id, "id-2");

        // Removing an unknown ID does not submit an event.
        manager.remove("unknown");
        manager.clear();
        assert!(manager.export().states.is_empty());

        // Every change was submitted.
        let mut count = 0;
        while updates.try_recv().is_ok() {
            count += 1;
        }
        assert_eq!(count, 5, "add, replace, add, remove, clear");
    }
}
