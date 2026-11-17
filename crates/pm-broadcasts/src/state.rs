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

/// Go marshals `BroadcastState` as a struct (`{"Read": "<rfc3339>"}`): the
/// `Read` field is a `time.Time`, which encodes as an RFC3339 string. Older
/// Rust records stored the bare string directly; keep accepting that shape on
/// read for backwards compatibility.
#[derive(Deserialize)]
#[serde(untagged)]
enum BroadcastStateRepr {
    Object {
        #[serde(rename = "Read")]
        read: String,
    },
    String(String),
}

impl Serialize for BroadcastState {
    fn serialize<S: Serializer>(&self, serializer: S) -> std::result::Result<S::Ok, S::Error> {
        use serde::ser::SerializeStruct;
        let mut state = serializer.serialize_struct("BroadcastState", 1)?;
        state.serialize_field("Read", &gotime::format_rfc3339(self.read))?;
        state.end()
    }
}

impl<'de> Deserialize<'de> for BroadcastState {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> std::result::Result<Self, D::Error> {
        let repr = BroadcastStateRepr::deserialize(deserializer)?;
        let text = match repr {
            BroadcastStateRepr::Object { read } => read,
            BroadcastStateRepr::String(read) => read,
        };
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
