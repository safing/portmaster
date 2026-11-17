//! Tests of the database system: registry, controller, interface, hooks,
//! subscriptions and permissions.

use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};

use pm_database::{
    now, DatabaseInfo, Databases, Error, Hook, MemoryStorage, Options, Query, Record,
};
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
        meta: Meta::default(),
        format: pm_dsd::format::JSON,
        data: pm_dsd::gojson::to_vec(&payload).unwrap(),
    }
}

fn setup(tag: &str) -> (PathBuf, Arc<Databases>) {
    let dir = temp_dir(tag);
    let dbs = Arc::new(Databases::new(&dir).unwrap());
    dbs.register(DatabaseInfo {
        name: "core".to_string(),
        description: "core test database".to_string(),
        storage_type: "sqlite".to_string(),
        shadow_delete: false,
    })
    .unwrap();
    (dir, dbs)
}

#[test]
fn registry_validation_and_lazy_open() {
    let dir = temp_dir("registry");
    let dbs = Arc::new(Databases::new(&dir).unwrap());

    let info = |name: &str| DatabaseInfo {
        name: name.to_string(),
        description: "test".to_string(),
        storage_type: "sqlite".to_string(),
        shadow_delete: false,
    };
    assert!(matches!(
        dbs.register(info("ab")),
        Err(Error::InvalidName(_))
    ));
    assert!(matches!(
        dbs.register(info("has space")),
        Err(Error::InvalidName(_))
    ));
    dbs.register(info("core")).unwrap();

    // Re-registering updates the description.
    let mut updated = info("core");
    updated.description = "updated".to_string();
    dbs.register(updated).unwrap();
    assert_eq!(dbs.databases()[0].description, "updated");
    assert!(dbs.is_registered("core"));

    // The storage is only opened on first use.
    assert!(!dir.join("core/sqlite/db.sqlite").exists());
    let controller = dbs.controller("core").unwrap();
    assert!(dir.join("core/sqlite/db.sqlite").exists());
    assert_eq!(controller.info().name, "core");
    assert!(!controller.read_only());

    assert!(matches!(
        dbs.controller("missing"),
        Err(Error::NotRegistered(_))
    ));

    dbs.shutdown().unwrap();
    assert!(matches!(
        dbs.internal_interface()
            .put(json_record("core:a", json!({"a": 1}))),
        Err(Error::ShuttingDown)
    ));
}

#[test]
fn injected_and_memory_storage() {
    let dbs = Arc::new(Databases::without_disk());
    dbs.register_injected("config", "config database").unwrap();

    // A registered injected database has no storage until one is injected.
    assert!(matches!(dbs.controller("config"), Err(Error::NotInjected)));

    let controller = dbs
        .inject("config", Box::new(MemoryStorage::new()))
        .unwrap();
    assert!(controller.injected());
    assert!(matches!(
        dbs.inject("config", Box::new(MemoryStorage::new())),
        Err(Error::AlreadyLoaded(_))
    ));

    let internal = dbs.internal_interface();
    internal
        .put(json_record("config:test/key", json!({"Value": 1})))
        .unwrap();
    assert_eq!(
        internal.get("config:test/key").unwrap().key.to_string(),
        "config:test/key"
    );

    dbs.register(DatabaseInfo {
        name: "other".to_string(),
        description: String::new(),
        storage_type: "sqlite".to_string(),
        shadow_delete: false,
    })
    .unwrap();
    assert!(matches!(
        dbs.inject("other", Box::new(MemoryStorage::new())),
        Err(Error::NotInjectedType)
    ));

    dbs.withdraw("config");
    assert!(matches!(dbs.controller("config"), Err(Error::NotInjected)));
}

#[test]
fn interface_permissions() {
    let (_dir, dbs) = setup("permissions");
    let internal = dbs.internal_interface();
    let external = dbs.interface(Options::default());
    let local_only = dbs.interface(Options {
        local: true,
        ..Options::default()
    });
    let internal_only = dbs.interface(Options {
        internal: true,
        ..Options::default()
    });

    internal
        .put(json_record("core:plain", json!({"a": 1})))
        .unwrap();
    assert!(external.get("core:plain").is_ok());

    internal
        .put(json_record("core:secret", json!({"a": 1})))
        .unwrap();
    internal.make_secret("core:secret").unwrap();
    assert!(matches!(
        external.get("core:secret"),
        Err(Error::PermissionDenied)
    ));
    assert!(matches!(
        local_only.get("core:secret"),
        Err(Error::PermissionDenied)
    ));
    assert!(internal_only.get("core:secret").is_ok());
    assert!(internal.get("core:secret").is_ok());

    // Existence is not hidden by permission checks, like in Go.
    assert!(external.exists("core:secret").unwrap());

    // Writing an unreadable record is denied.
    let secret = internal.get("core:secret").unwrap();
    assert!(matches!(external.put(secret), Err(Error::PermissionDenied)));
    assert!(matches!(
        external.delete("core:secret"),
        Err(Error::PermissionDenied)
    ));

    internal
        .put(json_record("core:crown", json!({"a": 1})))
        .unwrap();
    internal.make_crown_jewel("core:crown").unwrap();
    assert!(matches!(
        internal_only.get("core:crown"),
        Err(Error::PermissionDenied)
    ));
    assert!(local_only.get("core:crown").is_ok());

    // Secrecy and crown-jewel flags are visible in metadata.
    let meta = internal_only.get_meta("core:secret").unwrap();
    assert!(meta.secret);
    assert!(matches!(
        local_only.get_meta("core:secret"),
        Err(Error::PermissionDenied)
    ));
}

#[test]
fn options_apply_defaults() {
    let (_dir, dbs) = setup("options");
    let internal = dbs.internal_interface();
    let writer = dbs.interface(Options {
        always_make_secret: true,
        always_set_relative_expiry: 60,
        ..Options::default()
    });

    let before = now();
    writer
        .put(json_record("core:with-defaults", json!({"a": 1})))
        .unwrap();
    let meta = internal.get_meta("core:with-defaults").unwrap();
    assert!(meta.secret);
    assert!(meta.created >= before);
    assert!(meta.modified >= before);
    // Like Go, a relative expiry is stored as a negative `deleted` value and
    // only materialized into `expires` by the next metadata update.
    assert_eq!(meta.deleted, -60);
    assert_eq!(meta.expires, 0);

    let record = internal.get("core:with-defaults").unwrap();
    internal.put(record).unwrap();
    let meta = internal.get_meta("core:with-defaults").unwrap();
    assert!((meta.expires - (before + 60)).abs() <= 2, "{meta:?}");

    // put_new resets timestamps first.
    let mut record = json_record("core:new", json!({"a": 1}));
    record.meta = Meta {
        created: 1,
        modified: 2,
        ..Meta::default()
    };
    let before = now();
    writer.put_new(record).unwrap();
    let meta = internal.get_meta("core:new").unwrap();
    assert!(meta.created >= before, "{meta:?}");
    assert!(meta.modified >= before, "{meta:?}");

    // insert_value updates a single JSON attribute.
    internal
        .insert_value("core:with-defaults", "a", json!(2))
        .unwrap();
    let record = internal.get("core:with-defaults").unwrap();
    let accessor = record.accessor().unwrap();
    assert_eq!(accessor.get_int("a"), Some(2));

    // Type changes are rejected like sjson does.
    assert!(internal
        .insert_value("core:with-defaults", "a", json!("text"))
        .is_err());

    internal
        .set_absolute_expiry("core:new", 2_000_000_000)
        .unwrap();
    assert_eq!(
        internal.get_meta("core:new").unwrap().expires,
        2_000_000_000
    );
    internal.set_relative_expiry("core:new", 30).unwrap();
    let meta = internal.get_meta("core:new").unwrap();
    assert_eq!(meta.deleted, -30, "{meta:?}");
    assert!(!meta.is_deleted());

    // Deleting removes the record and a second delete reports not found.
    internal.delete("core:new").unwrap();
    assert!(matches!(internal.get("core:new"), Err(Error::NotFound)));
    assert!(matches!(internal.delete("core:new"), Err(Error::NotFound)));
}

#[test]
fn subscriptions_receive_updates() {
    let (_dir, dbs) = setup("subscriptions");
    let internal = dbs.internal_interface();
    let external = dbs.interface(Options::default());

    let mut subscription = internal.subscribe(Query::new("core:events/")).unwrap();
    internal
        .put(json_record("core:events/1", json!({"a": 1})))
        .unwrap();
    let update = subscription.try_recv().expect("update for events/1");
    assert_eq!(update.key.key, "events/1");

    internal
        .put(json_record("core:other/1", json!({"a": 1})))
        .unwrap();
    assert!(subscription.try_recv().is_none());

    let mut external_subscription = external.subscribe(Query::new("core:events/")).unwrap();
    internal
        .put(json_record("core:events/2", json!({"a": 2})))
        .unwrap();
    assert!(external_subscription.try_recv().is_some());

    // Secret updates are not delivered to interfaces without permission.
    internal.make_secret("core:events/2").unwrap();
    assert!(external_subscription.try_recv().is_none());
    while subscription.try_recv().is_some() {}

    subscription.cancel();
    internal
        .put(json_record("core:events/3", json!({"a": 3})))
        .unwrap();
    assert!(subscription.try_recv().is_none());
}

#[derive(Default)]
struct Recorder {
    calls: Mutex<Vec<String>>,
}

impl Recorder {
    fn record(&self, call: &str) {
        self.calls
            .lock()
            .expect("lock poisoned")
            .push(call.to_string());
    }

    fn calls(&self) -> Vec<String> {
        self.calls.lock().expect("lock poisoned").clone()
    }

    fn clear(&self) {
        self.calls.lock().expect("lock poisoned").clear();
    }
}

impl Hook for Recorder {
    fn uses_pre_get(&self) -> bool {
        true
    }

    fn pre_get(&self, db_key: &str) -> pm_database::Result<()> {
        self.record(&format!("pre_get:{db_key}"));
        Ok(())
    }

    fn uses_post_get(&self) -> bool {
        true
    }

    fn post_get(&self, record: Record) -> pm_database::Result<Record> {
        self.record("post_get");
        Ok(record)
    }

    fn uses_pre_put(&self) -> bool {
        true
    }

    fn pre_put(&self, record: Record) -> pm_database::Result<Record> {
        self.record("pre_put");
        Ok(record)
    }
}

struct Veto;

impl Hook for Veto {
    fn uses_pre_put(&self) -> bool {
        true
    }

    fn pre_put(&self, _record: Record) -> pm_database::Result<Record> {
        Err(Error::PermissionDenied)
    }
}

#[test]
fn hooks_run_in_order_and_can_be_cancelled() {
    use std::sync::Arc as StdArc;

    let (_dir, dbs) = setup("hooks");
    let internal = dbs.internal_interface();
    let controller = dbs.controller("core").unwrap();

    let recorder = StdArc::new(Recorder::default());
    let handle = controller.register_hook(Query::new("core:hooked/"), recorder.clone());
    let _veto = controller.register_hook(Query::new("core:vetoed/"), StdArc::new(Veto));

    internal
        .put(json_record("core:hooked/a", json!({"a": 1})))
        .unwrap();
    assert_eq!(recorder.calls(), vec!["pre_put"]);

    internal.get("core:hooked/a").unwrap();
    assert_eq!(
        recorder.calls(),
        vec!["pre_put", "pre_get:hooked/a", "post_get"]
    );

    // Hooks only run for records matching their query.
    internal
        .put(json_record("core:plain/a", json!({"a": 1})))
        .unwrap();
    internal.get("core:plain/a").unwrap();
    assert_eq!(recorder.calls().len(), 3);

    // A failing hook aborts the write.
    assert!(matches!(
        internal.put(json_record("core:vetoed/a", json!({"a": 1}))),
        Err(Error::PermissionDenied)
    ));
    assert!(matches!(
        internal.get("core:vetoed/a"),
        Err(Error::NotFound)
    ));

    handle.cancel();
    internal
        .put(json_record("core:hooked/b", json!({"a": 1})))
        .unwrap();
    assert_eq!(recorder.calls().len(), 3);
}

#[test]
fn shadow_delete_keeps_rows_until_purged() {
    let dir = temp_dir("shadow");
    let dbs = Arc::new(Databases::new(&dir).unwrap());
    for (name, shadow) in [("shadowed", true), ("direct", false)] {
        dbs.register(DatabaseInfo {
            name: name.to_string(),
            description: String::new(),
            storage_type: "sqlite".to_string(),
            shadow_delete: shadow,
        })
        .unwrap();
    }

    let internal = dbs.internal_interface();
    for db in ["shadowed", "direct"] {
        internal
            .put(json_record(&format!("{db}:a"), json!({"x": 1})))
            .unwrap();
        internal.delete(&format!("{db}:a")).unwrap();
        assert!(matches!(
            internal.get(&format!("{db}:a")),
            Err(Error::NotFound)
        ));
    }

    // The shadowed record is still in the file, the direct one is gone.
    {
        let conn = rusqlite::Connection::open(dir.join("shadowed/sqlite/db.sqlite")).unwrap();
        let (deleted, value): (i64, Option<Vec<u8>>) = conn
            .query_row(
                "SELECT deleted, value FROM records WHERE key = 'a'",
                [],
                |row| Ok((row.get(0)?, row.get(1)?)),
            )
            .unwrap();
        assert!(deleted > 0);
        assert!(value.is_none());
    }
    {
        let conn = rusqlite::Connection::open(dir.join("direct/sqlite/db.sqlite")).unwrap();
        let count: i64 = conn
            .query_row("SELECT COUNT(*) FROM records", [], |row| row.get(0))
            .unwrap();
        assert_eq!(count, 0);
    }

    // Maintenance with the default threshold only purges records deleted
    // before now, so a record deleted in this second survives one round.
    dbs.maintain_record_states().unwrap();
    dbs.controller("shadowed")
        .unwrap()
        .maintain_record_states(now() + 1)
        .unwrap();
    dbs.maintain().unwrap();
    dbs.maintain_thorough().unwrap();
    dbs.shutdown().unwrap();

    let conn = rusqlite::Connection::open(dir.join("shadowed/sqlite/db.sqlite")).unwrap();
    let count: i64 = conn
        .query_row("SELECT COUNT(*) FROM records", [], |row| row.get(0))
        .unwrap();
    assert_eq!(count, 0);
}

#[test]
fn read_only_storage_rejects_writes() {
    let dbs = Arc::new(Databases::without_disk());
    dbs.register_injected("readonly", "read only").unwrap();
    dbs.inject("readonly", Box::new(pm_database::InjectBase))
        .unwrap();

    let internal = dbs.internal_interface();
    assert!(matches!(
        internal.put(json_record("readonly:a", json!({"a": 1}))),
        Err(Error::ReadOnly)
    ));
    assert!(matches!(
        internal.get("readonly:a"),
        Err(Error::NotImplemented)
    ));
}

#[test]
fn invalid_queries_are_rejected() {
    let (_dir, dbs) = setup("invalid-query");
    let internal = dbs.internal_interface();
    internal
        .put(json_record("core:a", json!({"a": 1})))
        .unwrap();

    // An int condition with a non-numeric string is a build error. Go's
    // Interface.Query/Subscribe call q.Check() and return that error instead
    // of silently matching nothing.
    let bad = Query::new("core:").where_(pm_query::Condition::new("a", pm_query::EQUALS, "nope"));
    assert!(internal.query(&bad).is_err());
    assert!(internal.subscribe(bad).is_err());

    // A valid query still works.
    let good = Query::new("core:");
    assert_eq!(internal.query(&good).unwrap().len(), 1);
}

#[test]
fn subscriptions_drop_updates_when_feed_is_full() {
    let dbs = Arc::new(Databases::without_disk());
    dbs.register_injected("core", "core test").unwrap();
    dbs.inject("core", Box::new(MemoryStorage::new())).unwrap();
    let internal = dbs.internal_interface();

    let mut subscription = internal.subscribe(Query::new("core:events/")).unwrap();
    for i in 0..1005 {
        internal
            .put(json_record(&format!("core:events/{i}"), json!({"i": i})))
            .unwrap();
    }

    // The feed is buffered at 1000; the extra updates are dropped instead of
    // growing an unbounded queue.
    let mut received = 0;
    while subscription.try_recv().is_some() {
        received += 1;
    }
    assert_eq!(received, 1000);
    subscription.cancel();
}

#[test]
fn write_paths_do_not_run_read_hooks() {
    let (_dir, dbs) = setup("write-hooks");
    let controller = dbs.controller("core").unwrap();
    let recorder = Arc::new(Recorder::default());
    let hook = controller.register_hook(Query::new("core:hooked/"), recorder.clone());

    // A limited interface must check permissions on writes; Go does this with
    // a metadata-only GetMeta, so pre_get/post_get hooks must not run.
    let limited = dbs.interface(Options {
        local: true,
        internal: false,
        ..Options::default()
    });
    limited
        .put(json_record("core:hooked/a", json!({"a": 1})))
        .unwrap();
    assert_eq!(recorder.calls(), vec!["pre_put"]);

    recorder.clear();
    limited
        .set_absolute_expiry("core:hooked/a", now() + 100)
        .unwrap();
    assert_eq!(recorder.calls(), vec!["pre_put"]);

    hook.cancel();
}
