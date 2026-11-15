//! Behavior tests of the status module.

use std::sync::Arc;

use pm_database::{Databases, Query};
use pm_mgr::{State, StateUpdate};
use pm_netenv::{CaptivePortal, NetEnv, OnlineStatus};
use pm_notifications::{
    Notifications, ACTION_TYPE_OPEN_URL, STATE_ACTIVE, TYPE_INFO, TYPE_WARNING,
};
use pm_runtime::Registry;
use pm_status::{migrate_security_level_to_boolean, Status, SECURITY_LEVEL_NORMAL};
use serde_json::json;

struct TestModule {
    netenv: Arc<NetEnv>,
    notifications: Arc<Notifications>,
    status: Arc<Status>,
    registry: Registry,
}

impl TestModule {
    fn new() -> TestModule {
        let netenv = Arc::new(NetEnv::new());
        let notifications = Arc::new(Notifications::new());
        let status = Arc::new(Status::new(Arc::clone(&netenv), Arc::clone(&notifications)));
        let registry = Registry::new();
        status.register(&registry).unwrap();
        TestModule {
            netenv,
            notifications,
            status,
            registry,
        }
    }

    fn warning(&self, module: &str, id: &str) -> StateUpdate {
        StateUpdate::new(
            module,
            vec![State::new(id, "Warning title", pm_mgr::STATE_TYPE_WARNING)
                .with_message("Warning message")],
        )
    }
}

#[test]
fn states_become_notifications() {
    let module = TestModule::new();

    module
        .status
        .handle_module_status_update(module.warning("module-a", "module-a:warning-1"));

    let handle = module
        .notifications
        .get("module-a:warning-1")
        .expect("warning notification");
    let notification = handle.snapshot();
    assert_eq!(notification.event_id, "module-a:warning-1");
    assert_eq!(notification.type_, TYPE_WARNING);
    assert_eq!(notification.title, "Warning title");
    assert_eq!(notification.message, "Warning message");
    assert!(notification.show_on_system);
    assert_eq!(notification.state, STATE_ACTIVE);
    assert_eq!(notification.available_actions.len(), 1);
    assert_eq!(notification.available_actions[0].text, "Get Help");
    assert_eq!(
        notification.available_actions[0].type_,
        ACTION_TYPE_OPEN_URL
    );
    assert_eq!(
        notification.available_actions[0].payload,
        json!(pm_status::STATE_HELP_URL)
    );

    // Errors are shown on the system as well.
    module.status.handle_module_status_update(StateUpdate::new(
        "module-b",
        vec![State::new(
            "module-b:error-1",
            "Error",
            pm_mgr::STATE_TYPE_ERROR,
        )],
    ));
    assert!(
        module
            .notifications
            .get("module-b:error-1")
            .unwrap()
            .snapshot()
            .show_on_system
    );

    // Hints and undefined states are informational and have no action.
    module.status.handle_module_status_update(StateUpdate::new(
        "module-b",
        vec![
            State::new("module-b:hint-1", "Hint", pm_mgr::STATE_TYPE_HINT),
            State::new(
                "module-b:undefined",
                "Undefined",
                pm_mgr::STATE_TYPE_UNDEFINED,
            ),
        ],
    ));
    for id in ["module-b:hint-1", "module-b:undefined"] {
        let notification = module.notifications.get(id).unwrap().snapshot();
        assert_eq!(notification.type_, TYPE_INFO);
        assert!(!notification.show_on_system);
        assert!(notification.available_actions.is_empty());
    }

    // Re-using the update does not create a second notification.
    let before = module.notifications.all().len();
    module
        .status
        .handle_module_status_update(module.warning("module-a", "module-a:warning-1"));
    assert_eq!(module.notifications.all().len(), before);
}

#[test]
fn removed_states_delete_their_notifications() {
    let module = TestModule::new();
    module.status.handle_module_status_update(StateUpdate::new(
        "module-a",
        vec![
            State::new("state-1", "One", pm_mgr::STATE_TYPE_WARNING),
            State::new("state-2", "Two", pm_mgr::STATE_TYPE_ERROR),
        ],
    ));
    let handle = module.notifications.get("state-1").unwrap();

    // Only state-2 remains.
    module.status.handle_module_status_update(StateUpdate::new(
        "module-a",
        vec![State::new("state-2", "Two", pm_mgr::STATE_TYPE_ERROR)],
    ));
    assert!(module.notifications.get("state-1").is_none());
    assert!(handle.is_deleted());
    assert!(module.notifications.get("state-2").is_some());

    // A pre-created notification is adopted instead of duplicated.
    let precreated = module
        .notifications
        .notify(pm_notifications::Notification::info(
            "state-3",
            "Own",
            "Own message",
        ))
        .unwrap();
    module.status.handle_module_status_update(StateUpdate::new(
        "module-a",
        vec![
            State::new("state-2", "Two", pm_mgr::STATE_TYPE_ERROR),
            State::new("state-3", "Ignored name", pm_mgr::STATE_TYPE_WARNING),
        ],
    ));
    let adopted = module.notifications.get("state-3").unwrap();
    assert!(Arc::ptr_eq(&precreated, &adopted));
    assert_eq!(adopted.snapshot().title, "Own");
}

#[test]
fn system_status_is_built_and_served() {
    let module = TestModule::new();
    module
        .netenv
        .update_online_status(OnlineStatus::Online, None);

    module
        .status
        .handle_module_status_update(module.warning("module-b", "module-b:warning-1"));
    module.status.handle_module_status_update(StateUpdate::new(
        "module-a",
        vec![State::new(
            "module-a:error-1",
            "Error",
            pm_mgr::STATE_TYPE_ERROR,
        )],
    ));

    // Modules are sorted by name, the worst state wins by severity.
    let status = module.status.system_status();
    assert_eq!(status.online_status, OnlineStatus::Online);
    assert_eq!(status.modules.len(), 2);
    assert_eq!(status.modules[0].module, "module-a");
    assert_eq!(status.modules[1].module, "module-b");
    assert_eq!(status.worst_state.module, "module-a");
    assert_eq!(status.worst_state.state.id, "module-a:error-1");

    // The runtime provider serves the same record under the Go key.
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("runtime", "Runtime database")
        .unwrap();
    module
        .registry
        .inject_as_database(&databases, "runtime")
        .unwrap();

    let internal = databases.internal_interface();
    let record = internal.get("runtime:system/status").unwrap();
    assert_eq!(record.key.key, "system/status");
    assert!(record.meta.created > 0);
    let json: serde_json::Value = serde_json::from_slice(&record.data).unwrap();
    assert_eq!(json["OnlineStatus"], 5);
    assert_eq!(json["WorstState"]["Module"], "module-a");
    assert_eq!(json["WorstState"]["ID"], "module-a:error-1");

    let records = internal
        .query(&Query::new("runtime:system/status"))
        .unwrap();
    assert_eq!(records.len(), 1);
}

#[test]
fn updates_reach_runtime_subscribers() {
    let module = TestModule::new();
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("runtime", "Runtime database")
        .unwrap();
    module
        .registry
        .inject_as_database(&databases, "runtime")
        .unwrap();

    let internal = databases.internal_interface();
    let mut subscription = internal.subscribe(Query::new("runtime:")).unwrap();

    module
        .status
        .handle_module_status_update(module.warning("module-a", "state-1"));
    let update = subscription.try_recv().expect("status update");
    assert_eq!(update.key.key, "system/status");
    let json: serde_json::Value = serde_json::from_slice(&update.data).unwrap();
    assert_eq!(json["WorstState"]["Type"], "warning");

    // Without a runtime push function nothing breaks.
    let bare = Arc::new(Status::new(
        Arc::new(NetEnv::new()),
        Arc::new(Notifications::new()),
    ));
    bare.handle_module_status_update(StateUpdate::new(
        "module-a",
        vec![State::new("state-2", "Two", pm_mgr::STATE_TYPE_HINT)],
    ));
    assert_eq!(bare.states().len(), 1);
}

#[test]
fn equal_severities_keep_the_first_module() {
    let module = TestModule::new();
    for name in ["module-b", "module-a"] {
        module.status.handle_module_status_update(StateUpdate::new(
            name,
            vec![State::new(
                &format!("{name}:w"),
                "W",
                pm_mgr::STATE_TYPE_WARNING,
            )],
        ));
    }

    // States are applied in sorted module order; the first warning is kept.
    let status = module.status.system_status();
    assert_eq!(status.worst_state.module, "module-a");
    assert_eq!(status.worst_state.state.type_, pm_mgr::STATE_TYPE_WARNING);
}

#[test]
fn captive_portal_is_reported() {
    let module = TestModule::new();
    let portal = CaptivePortal::from_url("http://192.0.2.10/login");
    module
        .netenv
        .update_online_status(OnlineStatus::Portal, Some(portal.clone()));

    let status = module.status.system_status();
    assert_eq!(status.online_status, OnlineStatus::Portal);
    assert_eq!(status.captive_portal, Some(portal));
}

#[test]
fn security_level_migration() {
    use pm_config::OPT_TYPE_BOOL;
    use serde_json::Value;

    let cases: &[(Value, bool)] = &[
        (json!(0), false),
        (json!(1), true),
        (json!(2), false),
        (json!(3), true),
        (json!(4), false),
        (json!(5), true),
        (json!(1.0), true),
        (json!(2.9), false),
    ];
    for (value, expected) in cases {
        assert_eq!(
            migrate_security_level_to_boolean(OPT_TYPE_BOOL, value),
            Value::Bool(*expected),
            "{value}"
        );
    }

    // Other option types and non-numeric values are returned unchanged.
    assert_eq!(
        migrate_security_level_to_boolean(pm_config::OPT_TYPE_STRING, &json!(1)),
        json!(1)
    );
    assert_eq!(
        migrate_security_level_to_boolean(OPT_TYPE_BOOL, &json!("1")),
        json!("1")
    );
    assert_eq!(pm_status::SECURITY_LEVEL_EXTREME & SECURITY_LEVEL_NORMAL, 0);
}
