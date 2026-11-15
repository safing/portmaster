//! Compatibility tests for the SQLite storage against the Go-created fixture
//! and round-trip tests of the storage itself.

use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};

use pm_database::{now, Error, Query, Record, SqliteStorage, Storage};
use pm_dsd::format::{GZIP, JSON};
use pm_record::{Key, Meta};
use serde_json::{json, Value};

static COUNTER: AtomicU64 = AtomicU64::new(0);

fn temp_dir(tag: &str) -> PathBuf {
    let mut path = std::env::temp_dir();
    path.push(format!(
        "pm-db-{tag}-{}-{}",
        std::process::id(),
        COUNTER.fetch_add(1, Ordering::SeqCst)
    ));
    let _ = std::fs::remove_dir_all(&path);
    std::fs::create_dir_all(&path).unwrap();
    path
}

fn json_record(key: &str, payload: Value) -> Record {
    Record {
        key: Key::parse(key),
        meta: Meta {
            created: 100,
            modified: 200,
            ..Meta::default()
        },
        format: JSON,
        data: pm_dsd::gojson::to_vec(&payload).unwrap(),
    }
}

#[test]
fn reads_go_created_database() {
    let dir = temp_dir("fixture");
    let src = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../fixtures/m0/core_sample/db.sqlite"
    );
    std::fs::copy(src, dir.join("db.sqlite")).unwrap();

    let storage = SqliteStorage::open("core", &dir).unwrap();

    let fixture_path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../fixtures/m0/sqlite_records.json"
    );
    let text = std::fs::read_to_string(fixture_path).unwrap();
    let fixture: Value = serde_json::from_str(&text).unwrap();
    let cases = fixture["cases"].as_array().unwrap();
    assert_eq!(cases.len(), 6);

    for case in cases {
        let full_key = case["key"].as_str().unwrap();
        let db_key = full_key.split_once(':').unwrap().1;
        let record = storage
            .get(db_key)
            .unwrap_or_else(|err| panic!("{full_key}: {err}"));
        let expected = hex::decode(case["marshal_record"].as_str().unwrap()).unwrap();
        assert_eq!(
            record.marshal_record(),
            expected,
            "stored record differs from Go for {full_key}"
        );
    }
}

#[test]
fn query_honours_validity_and_permissions() {
    let dir = temp_dir("fixture-query");
    let src = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../fixtures/m0/core_sample/db.sqlite"
    );
    std::fs::copy(src, dir.join("db.sqlite")).unwrap();
    let storage = SqliteStorage::open("core", &dir).unwrap();

    let all = Query::new("core:");
    assert_eq!(storage.query(&all, true, true).unwrap().len(), 4);
    // The secret and crown jewel record is only visible with full permissions.
    assert_eq!(storage.query(&all, true, false).unwrap().len(), 3);
    assert_eq!(storage.query(&all, false, true).unwrap().len(), 3);
    assert_eq!(storage.query(&all, false, false).unwrap().len(), 3);
    // Deleted and expired records are always hidden.
    assert_eq!(
        storage
            .query(&Query::new("core:test/deleted"), true, true)
            .unwrap()
            .len(),
        0
    );
    assert_eq!(
        storage
            .query(&Query::new("core:test/expired"), true, true)
            .unwrap()
            .len(),
        0
    );

    let by_count = pm_query::parse_query("query core:test/ where Count > -50").unwrap();
    assert_eq!(storage.query(&by_count, true, true).unwrap().len(), 3);
    assert_eq!(storage.query(&by_count, true, false).unwrap().len(), 2);

    // A trailing space keeps the parser from cutting the last multi-byte rune.
    let by_name = pm_query::parse_query("query core:test/ where Name sameas ключ ").unwrap();
    let found = storage.query(&by_name, true, true).unwrap();
    assert_eq!(found.len(), 1);
    assert_eq!(found[0].key.to_string(), "core:test/unicode/ключ 🦀|x");

    // A CBOR record has no JSON accessor and cannot match a where clause.
    let cbor = storage
        .query(
            &pm_query::parse_query("query core:test/ where Count > -50").unwrap(),
            true,
            true,
        )
        .unwrap();
    assert!(cbor.iter().all(|record| !record.key.key.contains("cbor")));
}

#[test]
fn put_get_upsert_and_delete_roundtrip() {
    let dir = temp_dir("roundtrip");
    let storage = SqliteStorage::open("core", &dir).unwrap();

    let record = json_record("core:a/b", json!({"Name": "x", "Count": 7}));
    storage.put(record.clone()).unwrap();
    assert_eq!(storage.get("a/b").unwrap(), record);

    let mut updated = record.clone();
    updated.data = pm_dsd::gojson::to_vec(&json!({"Name": "y", "Count": 8})).unwrap();
    updated.meta.modified = 300;
    storage.put(updated.clone()).unwrap();
    assert_eq!(storage.get("a/b").unwrap(), updated);
    assert_eq!(
        storage
            .query(&Query::new("core:a/"), true, true)
            .unwrap()
            .len(),
        1
    );

    storage.delete("a/b").unwrap();
    assert!(matches!(storage.get("a/b"), Err(Error::StorageNotFound)));

    // Unsupported formats are rejected like Go's dsd validation.
    let mut bad = json_record("core:bad", json!({"a": 1}));
    bad.format = GZIP;
    assert!(matches!(storage.put(bad), Err(Error::IncompatibleFormat)));

    // AUTO is stored as JSON, empty payloads are stored as NULL.
    let mut auto = json_record("core:auto", json!({"a": 1}));
    auto.format = pm_dsd::format::AUTO;
    storage.put(auto).unwrap();
    assert_eq!(storage.get("auto").unwrap().format, JSON);

    let empty = Record {
        key: Key::parse("core:empty"),
        meta: Meta::default(),
        format: JSON,
        data: Vec::new(),
    };
    storage.put(empty).unwrap();
    let got = storage.get("empty").unwrap();
    assert_eq!(got.format, 0);
    assert!(got.data.is_empty());
}

#[test]
fn maintenance_purges_and_shadow_deletes() {
    let dir = temp_dir("maintenance");
    let storage = SqliteStorage::open("core", &dir).unwrap();
    let now = now();

    let mut normal = json_record("core:normal", json!({"a": 1}));
    normal.meta.expires = now + 100;
    storage.put(normal.clone()).unwrap();

    let mut expired = json_record("core:expired", json!({"a": 2}));
    expired.meta.expires = now - 100;
    storage.put(expired).unwrap();

    let mut deleted = json_record("core:deleted", json!({"a": 3}));
    deleted.meta.delete(now - 1_000);
    storage.put(deleted).unwrap();

    // Shadow delete keeps deleted records, marking expired ones as deleted.
    storage.maintain_record_states(now, true).unwrap();
    assert!(matches!(
        storage.get("deleted"),
        Err(Error::StorageNotFound)
    ));
    assert_eq!(storage.get("expired").unwrap().meta.deleted, now);
    assert_eq!(storage.get("normal").unwrap(), normal);

    // Immediate mode removes expired and deleted records.
    storage.maintain_record_states(now, false).unwrap();
    assert!(matches!(
        storage.get("expired"),
        Err(Error::StorageNotFound)
    ));
    assert_eq!(
        storage
            .query(&Query::new("core:"), true, true)
            .unwrap()
            .len(),
        1
    );

    storage.maintain().unwrap();
    storage.maintain_thorough().unwrap();
}

#[test]
fn migrations_are_idempotent() {
    let dir = temp_dir("migrations");

    {
        let storage = SqliteStorage::open("core", &dir).unwrap();
        storage.shutdown().unwrap();
    }
    // Reopening must not fail or duplicate migration rows.
    let storage = SqliteStorage::open("core", &dir).unwrap();
    drop(storage);

    let conn = rusqlite::Connection::open(dir.join("db.sqlite")).unwrap();
    let count: i64 = conn
        .query_row("SELECT COUNT(*) FROM gorp_migrations", [], |row| row.get(0))
        .unwrap();
    assert_eq!(count, 2);

    let ids: Vec<String> = conn
        .prepare("SELECT id FROM gorp_migrations ORDER BY id")
        .unwrap()
        .query_map([], |row| row.get(0))
        .unwrap()
        .collect::<Result<_, _>>()
        .unwrap();
    assert_eq!(ids, vec!["0_settings.sql", "1_initial.sql"]);

    let ddl: String = conn
        .query_row(
            "SELECT sql FROM sqlite_master WHERE name = 'gorp_migrations'",
            [],
            |row| row.get(0),
        )
        .unwrap();
    assert!(
        ddl.contains("\"id\" varchar(255) not null primary key"),
        "{ddl}"
    );
    assert!(ddl.contains("\"applied_at\" datetime"), "{ddl}");

    let journal: String = conn
        .query_row("PRAGMA journal_mode;", [], |row| row.get(0))
        .unwrap();
    assert_eq!(journal.to_lowercase(), "wal");
}

/// Writes a database with known contents for the Go verifier:
/// `go run ./tools/rust-fixtures -verify <dir>/core/sqlite`.
#[test]
fn writes_database_for_go_verifier() {
    use std::sync::Arc;

    use pm_database::{DatabaseInfo, Databases, Options};

    let root = std::env::var_os("PM_COMPAT_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|| std::env::temp_dir().join("pm-db-compat"));
    let location = root.join("core").join("sqlite");
    let _ = std::fs::remove_dir_all(&location);
    std::fs::create_dir_all(&location).unwrap();

    {
        let storage = SqliteStorage::open("core", &location).unwrap();

        let cbor =
            pm_dsd::dump(&json!({"Name": "cbor", "Count": 5}), pm_dsd::format::CBOR).unwrap();
        let records = vec![
            Record {
                key: Key::parse("core:compat/json"),
                meta: Meta {
                    created: 1_700_000_000,
                    modified: 1_700_000_100,
                    ..Meta::default()
                },
                format: JSON,
                data: pm_dsd::gojson::to_vec(&json!({"Name": "json <&>", "Count": -1})).unwrap(),
            },
            Record {
                key: Key::parse("core:compat/cbor"),
                meta: Meta {
                    created: 1_700_000_001,
                    modified: 1_700_000_101,
                    ..Meta::default()
                },
                format: pm_dsd::format::CBOR,
                data: cbor[1..].to_vec(),
            },
            Record {
                key: Key::parse("core:compat/deleted"),
                meta: Meta {
                    created: 1_700_000_002,
                    modified: 1_700_000_102,
                    deleted: 1_700_000_200,
                    ..Meta::default()
                },
                format: JSON,
                data: pm_dsd::gojson::to_vec(&json!({"Name": "gone"})).unwrap(),
            },
        ];
        for record in records {
            storage.put(record).unwrap();
        }

        let mut secret = json_record("core:compat/secret", json!({"Name": "secret"}));
        secret.meta.make_secret();
        secret.meta.make_crown_jewel();
        storage.put(secret).unwrap();
        storage.shutdown().unwrap();
    }

    // The same directory must be usable through the database system.
    let dbs = Arc::new(Databases::new(&root).unwrap());
    dbs.register(DatabaseInfo {
        name: "core".to_string(),
        description: "compat".to_string(),
        storage_type: "sqlite".to_string(),
        shadow_delete: true,
    })
    .unwrap();
    let internal = dbs.internal_interface();
    let record = internal.get("core:compat/json").unwrap();
    assert_eq!(record.meta.created, 1_700_000_000);

    // An interface without permissions reads normal records, but not secrets.
    let external = dbs.interface(Options::default());
    assert!(external.get("core:compat/json").is_ok());
    assert!(matches!(
        external.get("core:compat/secret"),
        Err(Error::PermissionDenied)
    ));

    // A shadow delete keeps the row but drops its payload.
    internal.delete("core:compat/secret").unwrap();
    assert!(matches!(
        internal.get("core:compat/secret"),
        Err(Error::NotFound)
    ));
    dbs.shutdown().unwrap();

    let conn = rusqlite::Connection::open(location.join("db.sqlite")).unwrap();
    let (format, value, deleted): (Option<i64>, Option<Vec<u8>>, i64) = conn
        .query_row(
            "SELECT format, value, deleted FROM records WHERE key = 'compat/secret'",
            [],
            |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)),
        )
        .unwrap();
    assert!(format.is_none() && value.is_none());
    assert!(deleted > 0);

    println!("compat database written to {}", location.display());
}
