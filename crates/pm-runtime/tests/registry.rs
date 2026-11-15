//! Port of the Go `base/runtime` registry tests, plus provider behavior.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use pm_database::{Databases, Query};
use pm_record::{Key, Meta, Record};
use pm_runtime::{
    Error, Registry, SimpleValueGetter, SimpleValueSetter, SingleRecord, ValueProvider,
};

fn text(record: &Record) -> String {
    String::from_utf8_lossy(&record.data).into_owned()
}

fn make_record(key: &str, value: &str) -> Record {
    Record {
        key: Key::new("runtime", key),
        meta: Meta::default(),
        format: pm_dsd::format::JSON,
        data: value.as_bytes().to_vec(),
    }
}

struct TestProvider {
    records: Vec<Record>,
    sets: AtomicUsize,
}

impl TestProvider {
    fn new(records: Vec<Record>) -> TestProvider {
        TestProvider {
            records,
            sets: AtomicUsize::new(0),
        }
    }
}

impl ValueProvider for TestProvider {
    fn get(&self, _key_or_prefix: &str) -> pm_runtime::Result<Vec<Record>> {
        Ok(self.records.clone())
    }

    fn set(&self, _record: Record) -> pm_runtime::Result<Option<Record>> {
        self.sets.fetch_add(1, Ordering::SeqCst);
        Err(Error::ReadOnly)
    }
}

fn test_registry() -> Registry {
    let registry = Registry::new();

    registry
        .register(
            "p1/",
            Arc::new(TestProvider::new(vec![
                make_record("p1/f1/v1", "p1.1"),
                make_record("p1/f2/v2", "p1.2"),
                make_record("p1/v3", "p1.3"),
            ])),
        )
        .unwrap();
    registry
        .register(
            "p2/f1",
            Arc::new(TestProvider::new(vec![
                make_record("p2/f1/v1", "p2.1"),
                make_record("p2/f1/f2/v2", "p2.2"),
                make_record("p2/f1/v3", "p2.3"),
            ])),
        )
        .unwrap();

    registry
}

#[test]
fn get_returns_exact_matches() {
    let registry = test_registry();

    let record = registry.get("p1/f1/v1").unwrap();
    assert_eq!(text(&record), "p1.1");
    let record = registry.get("p1/v3").unwrap();
    assert_eq!(text(&record), "p1.3");

    // Providers may return anything, only exact keys are served.
    assert!(matches!(registry.get("p1/v4"), Err(Error::NotFound)));
    assert!(matches!(
        registry.get("no-provider/foo"),
        Err(Error::NotFound)
    ));
}

#[test]
fn query_spans_providers() {
    let registry = test_registry();

    let records = registry
        .query(&Query::new("runtime:p"), true, true)
        .unwrap();
    assert_eq!(records.len(), 6);

    let records = registry
        .query(&Query::new("runtime:p1/f"), true, true)
        .unwrap();
    assert_eq!(records.len(), 2);
    assert_eq!(text(&records[0]), "p1.1");
    assert_eq!(text(&records[1]), "p1.2");

    // Unknown prefixes are reported like in Go.
    match registry.query(&Query::new("runtime:other/"), true, true) {
        Err(Error::KeyUnmanagedPrefix(prefix)) => assert_eq!(prefix, "other/"),
        other => panic!("unexpected result: {other:?}"),
    }
}

#[test]
fn registration_conflicts() {
    let registry = Registry::new();
    let cases = [
        ("runtime:foo/bar/bar", false),
        ("runtime:foo/bar/bar2", false),
        ("runtime:foo/bar", false),
        ("runtime:foo/bar", true),  // already used
        ("runtime:foo/bar/", true), // providers exist below
        ("runtime:foo/baz/", false),
        ("runtime:foo/baz2/", false),
        ("runtime:foo/baz3", false),
        ("runtime:foo/baz/bar", true),
    ];

    for (key, should_fail) in cases {
        let result = registry.register(key, Arc::new(TestProvider::new(Vec::new())));
        assert_eq!(result.is_err(), should_fail, "{key}");
        if let Err(Error::KeyTaken(found)) = result {
            assert!(!found.is_empty(), "{key}");
        }
    }

    assert_eq!(registry.registration_keys().len(), 6);
}

#[test]
fn provide_record_is_read_only_and_exact() {
    let record = make_record("status", "ok");
    let provider = SingleRecord::new(record.clone());

    let records = provider.get("status").unwrap();
    assert_eq!(records.len(), 1);
    assert_eq!(text(&records[0]), "ok");
    // A prefix is not an exact match, so nothing is returned.
    assert!(provider.get("stat").unwrap().is_empty());
    assert!(matches!(provider.set(record.clone()), Err(Error::ReadOnly)));

    // Simple providers have exactly one direction.
    let getter = SimpleValueGetter::new(|key| Ok(vec![make_record(key, "value")]));
    assert_eq!(getter.get("any").unwrap().len(), 1);
    assert!(matches!(getter.set(record.clone()), Err(Error::ReadOnly)));

    let setter = SimpleValueSetter::new(|record| Ok(Some(record)));
    assert!(matches!(setter.get("any"), Err(Error::WriteOnly)));
    assert_eq!(
        setter
            .set(record.clone())
            .unwrap()
            .map(|record| record.key.key),
        Some("status".to_string())
    );

    // Write-only providers hide their records from reads.
    let registry = Registry::new();
    registry
        .register(
            "write/",
            Arc::new(SimpleValueSetter::new(|record| Ok(Some(record)))),
        )
        .unwrap();
    let key = Key::new("runtime", "write/value");
    assert!(
        matches!(registry.get(&key.key), Err(Error::NotFound)),
        "write-only records are hidden"
    );
    let mut record = make_record("write/value", "new");
    record.key = key;
    assert!(registry.put(record).unwrap().is_some());

    // Read-only providers reject writes.
    registry
        .register(
            "read/",
            Arc::new(SingleRecord::new(make_record("read/value", "v"))),
        )
        .unwrap();
    assert!(matches!(
        registry.put(make_record("read/value", "v")),
        Err(Error::ReadOnly)
    ));

    // Unmanaged keys are reported.
    assert!(matches!(
        registry.put(make_record("unmanaged/value", "v")),
        Err(Error::KeyUnmanaged)
    ));
}

#[test]
fn injected_database_serves_and_receives() {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("runtime", "Runtime database")
        .unwrap();

    let registry = Registry::new();
    let mut provider = SingleRecord::new(make_record("system/status", "ok"));
    let push = registry
        .register("system/", Arc::new(provider.clone()))
        .unwrap();
    let controller = registry.inject_as_database(&databases, "runtime").unwrap();
    assert_eq!(registry.database_name(), "runtime");
    // Injecting twice fails.
    assert!(matches!(
        registry.inject_as_database(&databases, "runtime"),
        Err(Error::Injected)
    ));
    assert!(controller.injected());

    let internal = databases.internal_interface();
    let record = internal.get("runtime:system/status").unwrap();
    assert_eq!(text(&record), "ok");

    // A single-record provider only answers exact keys, like in Go.
    let query = internal.query(&Query::new("runtime:system/")).unwrap();
    assert!(query.is_empty());
    let query = internal
        .query(&Query::new("runtime:system/status"))
        .unwrap();
    assert_eq!(query.len(), 1);

    // Updates pushed by the provider reach subscribers.
    let mut subscription = internal.subscribe(Query::new("runtime:")).unwrap();
    provider.replace(make_record("system/status", "updated"));
    push(&[make_record("system/status", "updated")]);
    let update = subscription.try_recv().expect("runtime update");
    assert_eq!(update.key.key, "system/status");
    assert_eq!(text(&update), "updated");

    // Writes go through the provider, which is read-only here.
    assert!(internal.put(make_record("system/status", "nope")).is_err());
    // Deletes are not supported.
    assert!(internal.delete("runtime:system/status").is_err());
}

#[test]
fn module_injects_the_default_registry() {
    let databases = Arc::new(Databases::without_disk());
    let mut module = pm_runtime::Runtime::new();

    module.start(&databases).unwrap();
    assert_eq!(module.database_name(), "runtime");
    assert!(module.controller().is_some());

    let internal = databases.internal_interface();
    let query = internal.query(&Query::new("runtime:"));
    // The default registry is empty unless other tests registered to it, so a
    // query without providers reports an unmanaged key.
    assert!(query.is_err());

    module.stop();
    assert!(module.controller().is_none());
    assert_eq!(module.database_name(), "runtime");
}
