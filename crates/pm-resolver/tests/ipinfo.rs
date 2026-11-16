//! IP info records against the Go fixtures and the cache database.

use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_resolver::ipinfo::{IpInfoError, ip_info_interface_options};
use pm_resolver::rrcache::now_unix;
use pm_resolver::{IpInfo, IpInfoStore};
use serde_json::Value;

fn fixture() -> Value {
    let path = format!(
        "{}/../fixtures/m0/resolver.json",
        env!("CARGO_MANIFEST_DIR")
    );
    serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap()
}

fn store() -> (Arc<Databases>, IpInfoStore) {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .unwrap();
    databases
        .inject("cache", Box::new(MemoryStorage::new()))
        .unwrap();
    let store = IpInfoStore::new(databases.interface(ip_info_interface_options()));
    (databases, store)
}

#[test]
fn records_match_go_bytes() {
    let fx = fixture();
    let cases = fx["ip_infos"].as_array().unwrap();
    assert_eq!(cases.len(), 2);
    for case in cases {
        let json = case["json"].as_str().unwrap();
        let info: IpInfo = serde_json::from_str(json).unwrap();
        assert_eq!(info.key(), case["key"].as_str().unwrap());
        assert_eq!(String::from_utf8(info.to_json().unwrap()).unwrap(), json);
    }
}

#[test]
fn save_and_get_round_trip() {
    let fx = fixture();
    let json = fx["ip_infos"][1]["json"].as_str().unwrap();
    let mut info: IpInfo = serde_json::from_str(json).unwrap();

    let now = now_unix();
    let (databases, store) = store();
    assert!(matches!(
        store.get(&info.profile_id, &info.ip),
        Err(IpInfoError::NotFound)
    ));
    store.save_at(&mut info, now).unwrap();
    assert_eq!(store.get(&info.profile_id, &info.ip).unwrap(), info);

    // Expiry: max(now + 1 day, latest domain expiry) + 1 hour.
    let meta = databases
        .interface(ip_info_interface_options())
        .get_meta(&info.key())
        .unwrap();
    assert_eq!(meta.get_absolute_expiry(), now + 86400 + 3600);
    assert_ne!(meta.created, 0);

    // A later domain expiry wins.
    info.resolved_domains[0].expires = now + 10 * 86400;
    store.save_at(&mut info, now).unwrap();
    let meta = databases
        .interface(ip_info_interface_options())
        .get_meta(&info.key())
        .unwrap();
    assert_eq!(meta.get_absolute_expiry(), now + 10 * 86400 + 3600);
}

#[test]
fn unscoped_info_is_saved_globally() {
    let (_databases, store) = store();
    let mut info = IpInfo::new("", "192.0.2.7");
    store.save(&mut info).unwrap();
    assert_eq!(info.profile_id, "global");
    assert_eq!(
        store.get("global", "192.0.2.7").unwrap().key(),
        "cache:intel/ipInfo/global/192.0.2.7"
    );
}
