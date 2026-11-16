//! The netquery database (Go `netquery.Database`): live connections in an
//! in-memory `main` database and the persistent `history` database attached
//! from a file.
//!
//! Go keeps a pool of read-only shared-cache connections next to one write
//! connection. This port uses a single connection behind a mutex, which avoids
//! shared-cache lock errors; queries from the API must prepare as read-only
//! statements instead.

use std::collections::BTreeMap;
use std::path::Path;
use std::sync::{Mutex, MutexGuard};

use base64::Engine as _;
use rusqlite::types::{Value as Sql, ValueRef};
use rusqlite::Connection;
use serde_json::Value;

use crate::conn::Conn;
use crate::query::{generate_sql, Param, QueryRequest};
use crate::schema::{column, create_statement, ColumnKind};
use crate::timefmt::format_sqlite;

/// One result row; keys are sorted like a Go map.
pub type Row = BTreeMap<String, Value>;

/// Batch results by key, plus `"<key>: <error>"` for failed entries.
pub type BatchResult = (BTreeMap<String, Vec<Row>>, Vec<String>);

/// A database error.
#[derive(Debug, thiserror::Error)]
pub enum DbError {
    #[error(transparent)]
    Sqlite(#[from] rusqlite::Error),
    #[error(transparent)]
    Query(#[from] crate::QueryError),
    #[error("{0}")]
    Decode(String),
    #[error("statement is not read-only")]
    NotReadOnly,
}

/// The netquery database.
pub struct Database {
    conn: Mutex<Connection>,
}

impl Database {
    /// Opens the in-memory live database, attaches `history_file` (created if
    /// missing) and applies the schema (Go `NewInMemory` + `ApplyMigrations`).
    pub fn open(history_file: &Path) -> Result<Database, DbError> {
        let conn = Connection::open_in_memory()?;
        conn.execute(
            "ATTACH DATABASE ?1 AS history",
            [history_file.to_string_lossy().as_ref()],
        )?;
        let db = Database {
            conn: Mutex::new(conn),
        };
        db.apply_migrations()?;
        Ok(db)
    }

    fn lock(&self) -> MutexGuard<'_, Connection> {
        // A panic while holding the lock leaves the connection usable.
        self.conn.lock().unwrap_or_else(std::sync::PoisonError::into_inner)
    }

    fn apply_migrations(&self) -> Result<(), DbError> {
        let conn = self.lock();
        // journal_mode returns a row, so it cannot go through execute_batch.
        conn.query_row("PRAGMA history.journal_mode=WAL", [], |_| Ok(()))?;
        conn.execute_batch("PRAGMA history.synchronous=NORMAL;")?;
        for db in ["main", "history"] {
            conn.execute_batch(&create_statement(db, true))?;
            conn.execute_batch(&format!(
                "CREATE INDEX IF NOT EXISTS {db}.profile_id_index ON connections (profile);
                 CREATE INDEX IF NOT EXISTS {db}.started_time_index ON connections (strftime('%s', started)+0);
                 CREATE INDEX IF NOT EXISTS {db}.started_ended_time_index ON connections (strftime('%s', started)+0, strftime('%s', ended)+0) WHERE ended IS NOT NULL;"
            ))?;
        }
        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS main.bandwidth (
                conn_id TEXT NOT NULL,
                time INTEGER NOT NULL,
                incoming INTEGER NOT NULL,
                outgoing INTEGER NOT NULL,
                CONSTRAINT fk_conn_id
                    FOREIGN KEY(conn_id) REFERENCES connections(id)
                    ON DELETE CASCADE
            );
            CREATE INDEX IF NOT EXISTS main.bandwidth_conn_id_index ON bandwidth (conn_id);",
        )?;
        Ok(())
    }

    /// Runs an API query and decodes rows with the table schema (Go
    /// `QueryHandler`).
    pub fn query(&self, req: &QueryRequest) -> Result<Vec<Row>, DbError> {
        let (sql, params) = generate_sql(req)?;
        self.run_read(&sql, &params, true)
    }

    /// Runs several API queries (Go `BatchQueryHandler`). Go decodes batch
    /// rows without the schema: bools stay integers, times stay text and
    /// blobs become base64. Failed entries are reported as `"<key>: <error>"`
    /// and keep an empty result.
    pub fn query_batch(
        &self,
        reqs: &BTreeMap<String, QueryRequest>,
    ) -> Result<BatchResult, DbError> {
        let mut generated = Vec::with_capacity(reqs.len());
        for (key, req) in reqs {
            generated.push((key, generate_sql(req)?));
        }
        let mut out = BTreeMap::new();
        let mut errors = Vec::new();
        for (key, (sql, params)) in generated {
            match self.run_read(&sql, &params, false) {
                Ok(rows) => {
                    out.insert(key.clone(), rows);
                }
                Err(e) => {
                    errors.push(format!("{key}: {e}"));
                    out.insert(key.clone(), Vec::new());
                }
            }
        }
        Ok((out, errors))
    }

    fn run_read(
        &self,
        sql: &str,
        params: &BTreeMap<String, Param>,
        with_schema: bool,
    ) -> Result<Vec<Row>, DbError> {
        let conn = self.lock();
        let mut stmt = conn.prepare(sql)?;
        if !stmt.readonly() {
            return Err(DbError::NotReadOnly);
        }
        for (name, p) in params {
            let Some(idx) = stmt.parameter_index(name)? else {
                continue;
            };
            match p {
                Param::Text(s) => stmt.raw_bind_parameter(idx, s)?,
                Param::Float(f) => stmt.raw_bind_parameter(idx, f)?,
                Param::Bool(b) => stmt.raw_bind_parameter(idx, b)?,
            }
        }
        let names: Vec<String> = stmt.column_names().map(str::to_string).collect();
        let mut rows = stmt.raw_query();
        let mut out = Vec::new();
        while let Some(row) = rows.next()? {
            let mut map = Row::new();
            for (i, name) in names.iter().enumerate() {
                let raw = row.get_ref(i)?;
                let value = if with_schema {
                    decode_with_schema(name, raw)?
                } else {
                    decode_plain(raw)
                };
                map.insert(name.clone(), value);
            }
            out.push(map);
        }
        Ok(out)
    }

    /// Inserts or updates a connection (Go `Database.Save`).
    pub fn save(&self, conn: &Conn, enable_history: bool) -> Result<(), DbError> {
        let params = conn.params();
        let cols: Vec<&str> = params.iter().map(|(k, _)| *k).collect();
        let placeholders: Vec<String> = cols.iter().map(|c| format!(":{c}")).collect();
        let sets: Vec<String> = cols.iter().map(|c| format!("{c} = :{c}")).collect();
        let named: Vec<(String, &Sql)> = params.iter().map(|(k, v)| (format!(":{k}"), v)).collect();
        let named_refs: Vec<(&str, &dyn rusqlite::ToSql)> = named
            .iter()
            .map(|(k, v)| (k.as_str(), *v as &dyn rusqlite::ToSql))
            .collect();

        let conn = self.lock();
        let dbs: &[&str] = if enable_history { &["main", "history"] } else { &["main"] };
        for db in dbs {
            let sql = format!(
                "INSERT INTO {db}.connections ({}) VALUES({}) ON CONFLICT(id) DO UPDATE SET {}",
                cols.join(", "),
                placeholders.join(", "),
                sets.join(", ")
            );
            conn.execute(&sql, named_refs.as_slice())?;
        }
        Ok(())
    }

    /// Adds byte counters and logs a bandwidth sample (Go `UpdateBandwidth`).
    /// All statements are attempted; the first error is returned.
    pub fn update_bandwidth(
        &self,
        enable_history: bool,
        process_key: &str,
        conn_id: &str,
        bytes_received: u64,
        bytes_sent: u64,
        now_unix: i64,
    ) -> Result<(), DbError> {
        let id = format!("{process_key}-{conn_id}");
        let (rx, tx) = (sat_i64(bytes_received), sat_i64(bytes_sent));
        let conn = self.lock();
        let mut first_err: Option<rusqlite::Error> = None;
        let dbs: &[&str] = if enable_history { &["main", "history"] } else { &["main"] };
        for db in dbs {
            let sql = format!(
                "UPDATE {db}.connections SET bytes_received = (bytes_received + :bytes_received), bytes_sent = (bytes_sent + :bytes_sent) WHERE id = :id"
            );
            if let Err(e) = conn.execute(
                &sql,
                rusqlite::named_params! {":bytes_received": rx, ":bytes_sent": tx, ":id": id},
            ) {
                first_err.get_or_insert(e);
            }
        }
        if let Err(e) = conn.execute(
            "INSERT INTO main.bandwidth (conn_id, time, incoming, outgoing) VALUES(:id, :time, :bytes_received, :bytes_sent)",
            rusqlite::named_params! {":id": id, ":time": now_unix, ":bytes_received": rx, ":bytes_sent": tx},
        ) {
            first_err.get_or_insert(e);
        }
        first_err.map_or(Ok(()), |e| Err(e.into()))
    }

    /// Deletes ended live connections older than `threshold_unix` and their
    /// bandwidth samples; returns how many were deleted (Go `Cleanup`).
    pub fn cleanup(&self, threshold_unix: i64) -> Result<usize, DbError> {
        let threshold = format_sqlite(threshold_unix);
        let filter = "WHERE ended IS NOT NULL AND datetime(ended) < datetime(:threshold)";
        let conn = self.lock();
        conn.execute(
            &format!("DELETE FROM main.bandwidth WHERE conn_id IN (SELECT id FROM main.connections {filter})"),
            rusqlite::named_params! {":threshold": threshold},
        )?;
        let n = conn.execute(
            &format!("DELETE FROM main.connections {filter}"),
            rusqlite::named_params! {":threshold": threshold},
        )?;
        Ok(n)
    }

    /// Counts rows in live and history (Go `CountRows`).
    pub fn count_rows(&self) -> Result<i64, DbError> {
        let conn = self.lock();
        Ok(conn.query_row(
            "SELECT COUNT(*) AS count FROM (SELECT * FROM main.connections UNION SELECT * from history.connections)",
            [],
            |r| r.get::<_, i64>(0),
        )?)
    }

    /// Go `RemoveAllHistoryData`.
    pub fn remove_all_history(&self) -> Result<(), DbError> {
        self.lock().execute("DELETE FROM history.connections", [])?;
        Ok(())
    }

    /// Go `RemoveHistoryForProfile`.
    pub fn remove_history_for_profile(&self, profile_id: &str) -> Result<(), DbError> {
        self.lock().execute(
            "DELETE FROM history.connections WHERE profile = :profile",
            rusqlite::named_params! {":profile": profile_id},
        )?;
        Ok(())
    }

    /// Go `MigrateProfileID`.
    pub fn migrate_profile_id(&self, from: &str, to: &str) -> Result<(), DbError> {
        self.lock().execute(
            "UPDATE history.connections SET profile = :to WHERE profile = :from",
            rusqlite::named_params! {":from": from, ":to": to},
        )?;
        Ok(())
    }

    /// Go `MarkAllHistoryConnectionsEnded`. Go formats `now` in local time while
    /// every other time is UTC; this port writes UTC.
    pub fn mark_all_history_connections_ended(&self, now_unix: i64) -> Result<(), DbError> {
        self.lock().execute(
            "UPDATE history.connections SET active = FALSE, ended = :ended WHERE active = TRUE",
            rusqlite::named_params! {":ended": format_sqlite(now_unix)},
        )?;
        Ok(())
    }

    /// Deletes history outside each profile's retention (Go `CleanupHistory`).
    /// `retention_days` maps a stored profile ID (`local/<id>`) to its
    /// `history/keep` days; 0 disables cleanup for that profile. Errors of
    /// single profiles are collected.
    pub fn cleanup_history(
        &self,
        now_unix: i64,
        retention_days: impl Fn(&str) -> i64,
    ) -> Result<Vec<String>, DbError> {
        let profiles: Vec<String> = {
            let conn = self.lock();
            let mut stmt = conn.prepare("SELECT DISTINCT profile FROM history.connections")?;
            let rows = stmt.query_map([], |r| r.get(0))?;
            rows.collect::<Result<_, _>>()?
        };
        let mut errors = Vec::new();
        for profile in profiles {
            let days = retention_days(&profile);
            if days <= 0 {
                continue;
            }
            let threshold = now_unix.saturating_sub(days.saturating_mul(86_400));
            let res = self.lock().execute(
                "DELETE FROM history.connections WHERE profile = :profile AND active = FALSE AND datetime(started) < datetime(:threshold)",
                rusqlite::named_params! {":profile": profile, ":threshold": format_sqlite(threshold)},
            );
            if let Err(e) = res {
                errors.push(format!("profile {profile}: {e}"));
            }
        }
        Ok(errors)
    }
}

fn sat_i64(v: u64) -> i64 {
    i64::try_from(v).unwrap_or(i64::MAX)
}

/// Go decoding into `map[string]any` without a schema.
fn decode_plain(raw: ValueRef<'_>) -> Value {
    match raw {
        ValueRef::Null => Value::Null,
        ValueRef::Integer(i) => Value::from(i),
        ValueRef::Real(f) => serde_json::Number::from_f64(f).map_or(Value::Null, Value::Number),
        ValueRef::Text(t) => Value::String(String::from_utf8_lossy(t).into_owned()),
        ValueRef::Blob(b) => Value::String(base64::engine::general_purpose::STANDARD.encode(b)),
    }
}

fn type_name(raw: ValueRef<'_>) -> &'static str {
    match raw {
        ValueRef::Null => "NULL",
        ValueRef::Integer(_) => "INTEGER",
        ValueRef::Real(_) => "FLOAT",
        ValueRef::Text(_) => "TEXT",
        ValueRef::Blob(_) => "BLOB",
    }
}

/// Go `decodeIntoMap` with the connections schema: columns named like a
/// schema column are decoded as that Go field type.
fn decode_with_schema(name: &str, raw: ValueRef<'_>) -> Result<Value, DbError> {
    let Some(col) = column(name) else {
        return Ok(decode_plain(raw));
    };
    let mismatch = || {
        DbError::Decode(format!(
            "failed to decode column {name}: decode: unexpected column type {} for column {name}",
            type_name(raw)
        ))
    };

    if col.is_time {
        return match raw {
            ValueRef::Null if col.nullable => Ok(Value::Null),
            ValueRef::Null => Ok(Value::String("0001-01-01T00:00:00Z".into())),
            ValueRef::Text(t) => {
                let s = String::from_utf8_lossy(t);
                let unix = crate::timefmt::parse_sqlite(&s).ok_or_else(|| {
                    DbError::Decode(format!("failed to decode column {name}: failed to parse {s:?}"))
                })?;
                Ok(Value::String(crate::timefmt::format_rfc3339(unix)))
            }
            ValueRef::Integer(i) => Ok(Value::String(crate::timefmt::format_rfc3339(i))),
            _ => Err(mismatch()),
        };
    }

    if let ValueRef::Null = raw {
        if col.nullable {
            return Ok(Value::Null);
        }
        // Zero value of the Go field type.
        return Ok(match col.kind {
            ColumnKind::Text => Value::String(String::new()),
            ColumnKind::Integer => Value::from(0),
            ColumnKind::Float => Value::from(0.0),
            ColumnKind::Bool => Value::Bool(false),
            ColumnKind::Blob => Value::Null,
        });
    }

    match (col.kind, raw) {
        (ColumnKind::Text, ValueRef::Text(t)) => Ok(Value::String(String::from_utf8_lossy(t).into_owned())),
        (ColumnKind::Bool, ValueRef::Integer(i)) => Ok(Value::Bool(i != 0)),
        (ColumnKind::Float, ValueRef::Real(f)) => {
            Ok(serde_json::Number::from_f64(f).map_or(Value::Null, Value::Number))
        }
        (ColumnKind::Integer, ValueRef::Integer(i)) => Ok(Value::from(i)),
        // extra_data is json.RawMessage: Go embeds the bytes as JSON.
        (ColumnKind::Blob, ValueRef::Blob(b)) => {
            if b.is_empty() {
                return Ok(Value::Null);
            }
            serde_json::from_slice(b)
                .map_err(|e| DbError::Decode(format!("column {name} holds invalid JSON: {e}")))
        }
        _ => Err(mismatch()),
    }
}
