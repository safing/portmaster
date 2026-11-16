//! Broadcast state records, ported from Go `service/broadcasts/state.go`.

use std::collections::BTreeMap;

use pm_database::Interface;
use pm_dsd::gotime;
use pm_record::{Key, Meta, Record};
use serde::{Deserialize, Deserializer, Serialize, Serializer};

use crate::{Error, Result};

/// Database key of the broadcast states record.
pub const BROADCAST_STATES_DB_KEY: &str = "core:broadcasts/state";

/// Serializable payload of the `core:broadcasts/state` record.
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct BroadcastStates {
    #[serde(rename = "States")]
    pub states: BTreeMap<String, BroadcastState>,
}

/// State of a single broadcast notification.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct BroadcastState {
    /// Unix seconds of the acknowledgement; 0 is Go's zero time.
    pub read: i64,
}

impl Serialize for BroadcastState {
    fn serialize<S: Serializer>(&self, serializer: S) -> std::result::Result<S::Ok, S::Error> {
        serializer.serialize_str(&gotime::format_rfc3339(self.read))
    }
}

impl<'de> Deserialize<'de> for BroadcastState {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> std::result::Result<Self, D::Error> {
        let text = String::deserialize(deserializer)?;
        Ok(BroadcastState {
            read: gotime::parse_rfc3339(&text).unwrap_or(0),
        })
    }
}

impl BroadcastStates {
    /// Reads the states from the database, like Go `getBroadcastStates`.
    pub fn get(db: &Interface) -> Result<BroadcastStates> {
        let record = db.get(BROADCAST_STATES_DB_KEY)?;
        Ok(record.decode::<BroadcastStates>()?)
    }

    /// Reads the states, creating a fresh empty record when none exists yet.
    pub fn get_or_new(db: &Interface) -> Result<BroadcastStates> {
        match BroadcastStates::get(db) {
            Ok(states) => Ok(states),
            Err(Error::Database(pm_database::Error::NotFound)) => Ok(BroadcastStates::default()),
            Err(error) => Err(error),
        }
    }

    /// Persists the states under `core:broadcasts/state`, like Go `save`.
    pub fn save(&self, db: &Interface) -> Result<()> {
        let record = Record::from_typed(Key::parse(BROADCAST_STATES_DB_KEY), Meta::default(), self)?;
        db.put(record)?;
        Ok(())
    }
}
