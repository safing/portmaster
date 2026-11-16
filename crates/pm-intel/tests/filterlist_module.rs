//! Tests for the filter lists module glue (Go `filterlists/module.go`).

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use pm_database::{Databases, MemoryStorage};
use pm_intel::{
    FilterLists, FilterListsModule, FILTERLISTS_DISABLED_STATE, FILTERLISTS_STALE_DATA_STATE,
    FILTERLISTS_UPDATE_FAILED_STATE,
};
use pm_mgr::StateMgr;
use pm_netenv::OnlineStatus;

fn new_cache() -> Arc<Databases> {
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("cache", "Cache database")
        .expect("register");
    databases
        .inject("cache", Box::new(MemoryStorage::new()))
        .expect("inject");
    databases
}

fn filter_lists(databases: &Arc<Databases>) -> Arc<FilterLists> {
    Arc::new(FilterLists::with_capacity(
        pm_intel::cache_interface(databases),
        1000,
        100,
        10,
        100,
        100,
    ))
}

fn module(lists: Arc<FilterLists>) -> FilterListsModule {
    FilterListsModule::new(lists, Arc::new(StateMgr::new("FilterLists")))
}

#[test]
fn starts_disabled_without_a_cache_version() {
    let databases = new_cache();
    let module = module(filter_lists(&databases));

    module.start().expect("start");

    assert!(!module.lists().is_loaded());
    let states = module.states().export();
    assert_eq!(states.states.len(), 1);
    assert_eq!(states.states[0].id, FILTERLISTS_DISABLED_STATE);
    assert_eq!(states.states[0].name, "Filter Lists Are Initializing");
}

#[test]
fn loads_the_bloom_filters_from_the_cache() {
    let databases = new_cache();
    let lists = filter_lists(&databases);
    lists.save_bloom_cache().expect("save bloom");
    lists.set_cache_version("2.0.0").expect("set version");

    let reloaded = filter_lists(&databases);
    let module = module(reloaded);
    module.start().expect("start");

    assert!(module.lists().is_loaded());
    assert!(module.states().export().states.is_empty());
}

#[test]
fn update_events_manage_the_failure_state() {
    let databases = new_cache();
    let module = module(filter_lists(&databases));

    let error = module
        .handle_resources_updated(|| Err("download failed".to_string()))
        .expect_err("update failed");
    assert_eq!(error.to_string(), "download failed");
    let states = module.states().export();
    assert_eq!(states.states.len(), 1);
    assert_eq!(states.states[0].id, FILTERLISTS_UPDATE_FAILED_STATE);
    assert_eq!(states.states[0].name, "Filter Lists Update Failed");
    assert!(states.states[0].message.contains("download failed"));

    // A successful check clears the failure state.
    module
        .handle_resources_updated(|| Ok(()))
        .expect("update ok");
    assert!(module.states().export().states.is_empty());
}

#[test]
fn existing_warnings_suppress_the_failure_state() {
    let databases = new_cache();
    let module = module(filter_lists(&databases));
    module.warn_about_disabled_filter_lists();

    module
        .handle_resources_updated(|| Err("boom".to_string()))
        .expect_err("update failed");

    let states = module.states().export();
    assert_eq!(
        states.states.len(),
        1,
        "the warning state is kept, no second one is added"
    );
    assert_eq!(states.states[0].id, FILTERLISTS_DISABLED_STATE);
}

#[test]
fn update_events_can_be_ignored() {
    let databases = new_cache();
    let module = module(filter_lists(&databases));
    module.set_ignore_update_events(true);

    let calls = AtomicUsize::new(0);
    module
        .handle_resources_updated(|| {
            calls.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
        .expect("ignored");
    assert_eq!(calls.load(Ordering::SeqCst), 0);
}

#[test]
fn netenv_events_skip_offline_and_honor_the_ignore_flag() {
    let databases = new_cache();
    let module = module(filter_lists(&databases));
    let calls = AtomicUsize::new(0);

    // Ignored by default, like the Go init for tests.
    module
        .handle_online_status(OnlineStatus::Online, || {
            calls.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
        .expect("ignored");
    assert_eq!(calls.load(Ordering::SeqCst), 0);

    module.set_ignore_netenv_events(false);
    module
        .handle_online_status(OnlineStatus::Offline, || {
            calls.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
        .expect("offline");
    assert_eq!(calls.load(Ordering::SeqCst), 0, "offline does not update");

    module
        .handle_online_status(OnlineStatus::Online, || {
            calls.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
        .expect("online");
    assert_eq!(calls.load(Ordering::SeqCst), 1);
}

#[test]
fn stale_data_warning_matches_go() {
    let databases = new_cache();
    let module = module(filter_lists(&databases));

    module.warn_about_stale_data("disk full");
    let states = module.states().export();
    assert_eq!(states.states[0].id, FILTERLISTS_STALE_DATA_STATE);
    assert_eq!(states.states[0].name, "Filter Lists May Overblock");
    assert!(states.states[0].message.contains("disk full"));
}
