//! The injected `network` database, a port of Go `network/database.go`.
//!
//! Keys:
//! * processes: `network:tree/<PID>`
//! * DNS requests: `network:tree/<PID>/dns/<ID>`
//! * IP connections: `network:tree/<PID>/ip/<ID>`

use std::sync::Arc;

use pm_database::{Databases, Error, Result, Storage};
use pm_process::Process;
use pm_query::Query;
use pm_record::{Key, Meta, Record};

use crate::connection::SharedConnection;
use crate::network::{Network, DB_SCOPE_DNS, DB_SCOPE_IP};

/// Name of the database.
pub const DATABASE_NAME: &str = "network";
/// Description of the database.
pub const DATABASE_DESCRIPTION: &str = "Network and Firewall Data";

/// A parsed database key (without the database name).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ParsedKey {
    pub process_key: String,
    pub scope: String,
    pub id: String,
}

/// Parses a database key or query prefix, like Go `parseDBKey`.
///
/// Returns `None` for invalid keys. A prefix without a process key (such as
/// `tree/`) is valid and has an empty process key.
pub fn parse_db_key(key: &str) -> Option<ParsedKey> {
    let segments: Vec<&str> = key.split('/').collect();
    let mut parsed = ParsedKey {
        process_key: String::new(),
        scope: String::new(),
        id: String::new(),
    };

    // Keys have 2 to 4 segments.
    match segments.len() {
        1 => return Some(parsed),
        2..=4 => {}
        _ => return None,
    }
    if segments.len() == 4 {
        parsed.id = segments[3].to_string();
    }
    if segments.len() >= 3 {
        // The empty scope matches a trailing slash in a query prefix.
        match segments[2] {
            "" | DB_SCOPE_DNS | DB_SCOPE_IP => parsed.scope = segments[2].to_string(),
            _ => return None,
        }
    }
    parsed.process_key = segments[1].to_string();
    Some(parsed)
}

/// Builds the database record of a process.
pub fn process_record(process: &Process, now: i64) -> Option<Record> {
    let data = pm_dsd::gojson::to_vec(process).ok()?;
    Some(Record {
        key: Key::new(DATABASE_NAME, &process.scoped_key()),
        meta: Meta {
            created: process.first_seen,
            modified: now,
            ..Meta::default()
        },
        format: pm_dsd::format::JSON,
        data,
    })
}

/// The storage of the `network` database.
pub struct NetworkStorage {
    network: Arc<Network>,
}

impl NetworkStorage {
    pub fn new(network: Arc<Network>) -> NetworkStorage {
        NetworkStorage { network }
    }

    fn connection_record(conn: &SharedConnection) -> Option<Record> {
        let conn = conn.lock().expect("lock poisoned");
        if !conn.data_complete {
            return None;
        }
        conn.to_record()
    }
}

impl Storage for NetworkStorage {
    fn get(&self, key: &str) -> Result<Record> {
        let key = key.strip_prefix("network:").unwrap_or(key);
        let parsed = parse_db_key(key).ok_or(Error::StorageNotFound)?;
        if parsed.process_key.is_empty() {
            return Err(Error::StorageNotFound);
        }

        let record = match parsed.scope.as_str() {
            DB_SCOPE_DNS => self
                .network
                .dns_conns
                .get(&parsed.id)
                .and_then(|conn| Self::connection_record(&conn)),
            DB_SCOPE_IP => self
                .network
                .conns
                .get(&parsed.id)
                .and_then(|conn| Self::connection_record(&conn)),
            _ => self
                .network
                .env
                .processes
                .process_from_storage(&parsed.process_key)
                .and_then(|process| process_record(&process, self.network.env.clock.now())),
        };
        record.ok_or(Error::StorageNotFound)
    }

    fn put(&self, _record: Record) -> Result<Record> {
        Err(Error::NotImplemented)
    }

    fn delete(&self, _key: &str) -> Result<()> {
        Err(Error::NotImplemented)
    }

    fn query(&self, query: &Query, _local: bool, _internal: bool) -> Result<Vec<Record>> {
        let mut records = Vec::new();
        let Some(parsed) = parse_db_key(&query.database_key_prefix()) else {
            return Ok(records);
        };
        let scope = parsed.scope.as_str();

        if parsed.process_key.is_empty() {
            let now = self.network.env.clock.now();
            for process in self.network.env.processes.all_processes() {
                if let Some(record) = process_record(&process, now) {
                    if query.matches(&record) {
                        records.push(record);
                    }
                }
            }
        }

        if scope.is_empty() || scope == DB_SCOPE_DNS {
            for conn in self.network.dns_conns.list() {
                if let Some(record) = Self::connection_record(&conn) {
                    if query.matches(&record) {
                        records.push(record);
                    }
                }
            }
        }

        if scope.is_empty() || scope == DB_SCOPE_IP {
            for conn in self.network.conns.list() {
                if let Some(record) = Self::connection_record(&conn) {
                    if query.matches(&record) {
                        records.push(record);
                    }
                }
            }
        }

        Ok(records)
    }

    fn injected(&self) -> bool {
        true
    }
}

/// Registers the `network` database, injects its storage and connects the
/// controller, like Go `registerAsDatabase`.
pub fn register_as_database(network: &Arc<Network>, databases: &Databases) -> Result<()> {
    databases.register_injected(DATABASE_NAME, DATABASE_DESCRIPTION)?;
    let controller = databases.inject(
        DATABASE_NAME,
        Box::new(NetworkStorage::new(Arc::clone(network))),
    )?;
    network.set_db_controller(controller);
    Ok(())
}
