//! SQLite storage, compatible with the Go implementation's file layout.
//!
//! The file is `<location>/db.sqlite` with WAL journaling. The schema, the
//! `gorp_migrations` bookkeeping table and the record encoding match
//! `base/database/storage/sqlite`, so Go and Rust can open each other's files.

use std::path::Path;
use std::sync::Mutex;
use std::time::Duration;

use pm_dsd::format::{AUTO, CBOR, GEN_CODE, JSON, MSG_PACK, RAW, YAML};
use pm_query::Query;
use pm_record::{Key, Meta, Record};
use rusqlite::{Connection, OptionalExtension, Row};

use crate::storage::{record_matches, Storage};
use crate::{now, Error, Result};

/// Migrations as `(id, sql)`, matching the Go migration files.
const MIGRATIONS: &[(&str, &str)] = &[
    ("0_settings.sql", "PRAGMA auto_vacuum = INCREMENTAL;"),
    (
        "1_initial.sql",
        "CREATE TABLE IF NOT EXISTS records (
    key TEXT PRIMARY KEY,

    format SMALLINT,
    value  BLOB,

    created    BIGINT NOT NULL,
    modified   BIGINT NOT NULL,
    expires    BIGINT DEFAULT 0 NOT NULL,
    deleted    BIGINT DEFAULT 0 NOT NULL,
    secret     BOOLEAN DEFAULT FALSE NOT NULL,
    crownjewel BOOLEAN DEFAULT FALSE NOT NULL
);",
    ),
];

const SELECT_COLUMNS: &str =
    "SELECT key, format, value, created, modified, expires, deleted, secret, crownjewel";

/// A database backed by a single SQLite file.
pub struct SqliteStorage {
    name: String,
    conn: Mutex<Connection>,
}

impl SqliteStorage {
    /// Opens (and migrates) `<location>/db.sqlite`.
    pub fn open(name: impl Into<String>, location: impl AsRef<Path>) -> Result<SqliteStorage> {
        let location = location.as_ref();
        std::fs::create_dir_all(location)?;
        let path = location.join("db.sqlite");

        let conn = Connection::open(&path)?;
        let _: String = conn.query_row("PRAGMA journal_mode=WAL;", [], |row| row.get(0))?;
        conn.execute_batch(
            "PRAGMA synchronous=NORMAL;
             PRAGMA cache_size=-10000;",
        )?;
        conn.busy_timeout(Duration::from_millis(3000))?;

        run_migrations(&conn)?;

        Ok(SqliteStorage {
            name: name.into(),
            conn: Mutex::new(conn),
        })
    }

    pub fn name(&self) -> &str {
        &self.name
    }

    fn with_conn<T>(&self, f: impl FnOnce(&Connection) -> Result<T>) -> Result<T> {
        let conn = self.conn.lock().expect("lock poisoned");
        f(&conn)
    }
}

fn validate_format(format: u8) -> Option<u8> {
    match format {
        AUTO => Some(JSON),
        RAW | CBOR | GEN_CODE | JSON | MSG_PACK | YAML => Some(format),
        _ => None,
    }
}

fn run_migrations(conn: &Connection) -> Result<()> {
    // Same DDL sql-migrate uses.
    conn.execute_batch(
        "CREATE TABLE IF NOT EXISTS \"gorp_migrations\" (
            \"id\" varchar(255) not null primary key,
            \"applied_at\" datetime
        );",
    )?;

    for (id, sql) in MIGRATIONS {
        let applied = conn
            .query_row("SELECT id FROM gorp_migrations WHERE id = ?1", [id], |_| {
                Ok(())
            })
            .optional()?
            .is_some();
        if applied {
            continue;
        }

        conn.execute_batch(sql)?;
        conn.execute(
            "INSERT INTO gorp_migrations (id, applied_at) VALUES (?1, ?2)",
            rusqlite::params![id, crate::time::utc_timestamp_now()],
        )?;
    }
    Ok(())
}

fn row_to_record(name: &str, row: &Row<'_>) -> rusqlite::Result<Record> {
    let key: String = row.get(0)?;
    let format: Option<i64> = row.get(1)?;
    let data: Option<Vec<u8>> = row.get(2)?;
    Ok(Record {
        key: Key::new(name, &key),
        meta: Meta {
            created: row.get(3)?,
            modified: row.get(4)?,
            expires: row.get(5)?,
            deleted: row.get(6)?,
            secret: row.get(7)?,
            cronjewel: row.get(8)?,
        },
        format: format.unwrap_or(0) as u8,
        data: data.unwrap_or_default(),
    })
}

impl Storage for SqliteStorage {
    fn get(&self, key: &str) -> Result<Record> {
        self.with_conn(|conn| {
            conn.query_row(
                &format!("{SELECT_COLUMNS} FROM records WHERE key = ?1"),
                [key],
                |row| row_to_record(&self.name, row),
            )
            .optional()?
            .ok_or(Error::StorageNotFound)
        })
    }

    fn put(&self, record: Record) -> Result<Record> {
        let format = validate_format(record.format).ok_or(Error::IncompatibleFormat)?;
        // Deleted records keep no format and no value, like in Go.
        let (format, value) = if record.meta.is_deleted() || record.data.is_empty() {
            (None, None)
        } else {
            (Some(i64::from(format)), Some(record.data.as_slice()))
        };

        self.with_conn(|conn| {
            conn.execute(
                "INSERT INTO records
                    (key, format, value, created, modified, expires, deleted, secret, crownjewel)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
                 ON CONFLICT(key) DO UPDATE SET
                    format = excluded.format,
                    value = excluded.value,
                    created = excluded.created,
                    modified = excluded.modified,
                    expires = excluded.expires,
                    deleted = excluded.deleted,
                    secret = excluded.secret,
                    crownjewel = excluded.crownjewel",
                rusqlite::params![
                    record.key.key,
                    format,
                    value,
                    record.meta.created,
                    record.meta.modified,
                    record.meta.expires,
                    record.meta.deleted,
                    record.meta.secret,
                    record.meta.cronjewel,
                ],
            )?;
            Ok(())
        })?;

        Ok(record)
    }

    fn delete(&self, key: &str) -> Result<()> {
        self.with_conn(|conn| {
            conn.execute("DELETE FROM records WHERE key = ?1", [key])?;
            Ok(())
        })
    }

    fn query(&self, query: &Query, local: bool, internal: bool) -> Result<Vec<Record>> {
        self.with_conn(|conn| {
            let like = format!("{}%", query.database_key_prefix());
            let mut stmt = conn.prepare(&format!(
                "{SELECT_COLUMNS} FROM records WHERE key LIKE ?1 ORDER BY rowid"
            ))?;
            let rows = stmt.query_map([like], |row| row_to_record(&self.name, row))?;

            let now = now();
            let mut records = Vec::new();
            for row in rows {
                let record = row?;
                if record_matches(&record, query, local, internal, now) {
                    records.push(record);
                }
            }
            Ok(records)
        })
    }

    fn maintain(&self) -> Result<()> {
        self.with_conn(|conn| {
            conn.execute_batch("PRAGMA incremental_vacuum(25);")?;
            Ok(())
        })
    }

    fn maintain_thorough(&self) -> Result<()> {
        self.with_conn(|conn| {
            conn.execute_batch("PRAGMA incremental_vacuum;")?;
            Ok(())
        })
    }

    fn maintain_record_states(&self, purge_deleted_before: i64, shadow_delete: bool) -> Result<()> {
        let now = now();
        self.with_conn(|conn| {
            if shadow_delete {
                conn.execute(
                    "UPDATE records SET format = NULL, value = NULL, deleted = ?1
                     WHERE deleted = 0 AND expires > 0 AND expires < ?1",
                    [now],
                )?;
                conn.execute(
                    "DELETE FROM records WHERE deleted > 0 AND deleted < ?1",
                    [purge_deleted_before],
                )?;
                return Ok(());
            }

            conn.execute(
                "DELETE FROM records WHERE expires > 0 AND expires < ?1",
                [now],
            )?;
            conn.execute("DELETE FROM records WHERE deleted > 0", [])?;
            Ok(())
        })
    }

    fn shutdown(&self) -> Result<()> {
        // Fold the WAL into the main file so the result is a single file.
        self.with_conn(|conn| {
            let _ = conn.execute_batch("PRAGMA wal_checkpoint(TRUNCATE);");
            Ok(())
        })
    }
}
