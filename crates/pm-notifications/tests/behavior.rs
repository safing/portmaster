//! Behavior tests of the notification registry, storage and config.

use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use pm_database::{Databases, Query};
use pm_notifications::{
    Notification, Notifications, NotificationsStorage, STATE_ACTIVE, STATE_EXECUTED,
    STATE_RESPONDED, TYPE_WARNING,
};
use serde_json::json;

fn registry() -> Arc<Notifications> {
    Arc::new(Notifications::new())
}

fn notification(id: &str) -> Notification {
    Notification::info(id, "Title", "Message")
}

#[test]
fn notify_sets_defaults_and_actions() {
    let notifications = registry();

    let handle = notifications
        .notify_info("test:info", "Title", "Message")
        .expect("notification saved");
    let notification = handle.snapshot();
    assert_eq!(notification.event_id, "test:info");
    assert_eq!(notification.state, STATE_ACTIVE);
    assert!(!notification.guid.is_empty());
    assert!(
        !notification.show_on_system,
        "info notifications stay in the app"
    );
    assert_eq!(notification.available_actions.len(), 1);
    assert_eq!(notification.available_actions[0].id, "ack");
    assert_eq!(notification.key(), "notifications:all/test:info");
    assert!(handle.meta().created > 0);
    assert_eq!(handle.meta().created, handle.meta().modified);

    // Warnings are shown on the system.
    let warning = notifications
        .notify_warning("test:warning", "Title", "Message")
        .unwrap()
        .snapshot();
    assert_eq!(warning.type_, TYPE_WARNING);
    assert!(warning.show_on_system);

    // System notifications can be disabled.
    notifications.set_use_system_notifications(false);
    let disabled = notifications
        .notify_error("test:error", "Title", "Message")
        .unwrap()
        .snapshot();
    assert!(!disabled.show_on_system);

    // Notifications without a title and message are ignored.
    assert!(notifications.notify(Notification::default()).is_none());
    assert!(notifications.get("test:info").is_some());
}

#[test]
fn event_ids_are_derived_per_instance() {
    let first = registry();
    let one = first
        .notify(Notification::info("", "Title", "Same message"))
        .unwrap()
        .snapshot();
    let two = first
        .notify(Notification::info("", "Title", "Same message"))
        .unwrap()
        .snapshot();
    assert!(one.event_id.starts_with("unknown:"));
    // The same message maps to the same event ID within one process.
    assert_eq!(one.event_id, two.event_id);

    let second = registry();
    let other = second
        .notify(Notification::info("", "Title", "Same message"))
        .unwrap()
        .snapshot();
    assert_ne!(one.event_id, other.event_id);
}

#[test]
fn delete_and_expiry() {
    let notifications = registry();
    let handle = notifications.notify_info("test:delete", "T", "M").unwrap();
    let expired = handle.expired();

    notifications.delete("test:delete");
    assert!(handle.is_deleted());
    assert!(notifications.get("test:delete").is_none());
    assert!(*expired.borrow());

    // Deleting twice is harmless.
    notifications.delete_handle(&handle, false);

    // Expired notifications are cleaned up.
    let expired_handle = notifications
        .notify(notification("test:expired").with_expiry(1_000))
        .unwrap();
    let active_handle = notifications
        .notify(notification("test:active").with_expiry(4_000_000_000))
        .unwrap();
    notifications.clean_expired();
    assert!(expired_handle.is_deleted());
    assert!(!active_handle.is_deleted());

    // Like Go, a notification that was modified less than ten seconds ago is
    // not updated.
    let modified = active_handle.meta().modified;
    notifications.update_expiry("test:active", 5_000_000_000);
    assert_eq!(
        active_handle.snapshot().expires,
        4_000_000_000,
        "fresh notifications are rate limited"
    );

    // Older notifications are updated.
    notifications.update_expiry_at("test:active", 5_000_000_000, modified + 11);
    assert_eq!(active_handle.snapshot().expires, 5_000_000_000);
    assert!(active_handle.meta().modified > modified);

    // Non-active notifications are never updated.
    let responding = notifications
        .notify(
            Notification::prompt("test:responding", "T", "M")
                .with_action(pm_notifications::Action::new("ack", "OK")),
        )
        .unwrap();
    notifications.select_action("test:responding", "ack");
    notifications.update_expiry_at("test:responding", 5_000_000_000, modified + 100);
    assert_eq!(responding.snapshot().expires, 0);
    assert_eq!(notifications.all().len(), 2);
}

#[test]
fn actions_are_executed_on_selection() {
    let notifications = registry();
    let handle = notifications
        .notify(
            Notification::prompt("test:prompt", "T", "M")
                .with_action(pm_notifications::Action::new("allow", "Allow"))
                .with_action(pm_notifications::Action::new("deny", "Deny")),
        )
        .unwrap();

    let calls = Arc::new(AtomicUsize::new(0));
    let counter = Arc::clone(&calls);
    handle.set_action_handler(Arc::new(move |notification| {
        // Go sets the state and the selected action before running the handler.
        assert_eq!(notification.state, STATE_RESPONDED);
        assert_eq!(notification.selected_action_id, "allow");
        counter.fetch_add(1, Ordering::SeqCst);
        Ok(())
    }));

    let response = handle.response();
    assert!(notifications.select_action("test:prompt", "allow"));
    assert_eq!(calls.load(Ordering::SeqCst), 1);
    assert_eq!(*response.borrow(), Some("allow".to_string()));

    let notification = handle.snapshot();
    assert_eq!(notification.state, STATE_EXECUTED);
    assert_eq!(notification.selected_action_id, "allow");

    // A second selection is ignored.
    assert!(!notifications.select_action("test:prompt", "deny"));
    assert!(!notifications.select_action("missing", "allow"));
}

#[test]
fn updates_from_the_database() {
    let notifications = registry();
    let handle = notifications
        .notify(
            Notification::prompt("test:update", "T", "M")
                .with_action(pm_notifications::Action::new("allow", "Allow")),
        )
        .unwrap();

    // Selecting an action through an update only marks it as responded when
    // nobody waits for the action, exactly like Go.
    let mut update = handle.snapshot();
    update.selected_action_id = "allow".to_string();
    let result = notifications
        .apply_update(update, "all/test:update")
        .expect("update applied");
    assert_eq!(result.state, STATE_RESPONDED);
    assert_eq!(result.selected_action_id, "allow");

    // With a listener the action is executed.
    let waiting = notifications
        .notify(
            Notification::prompt("test:listened", "T", "M")
                .with_action(pm_notifications::Action::new("allow", "Allow")),
        )
        .unwrap();
    let response = waiting.response();
    let mut update = waiting.snapshot();
    update.selected_action_id = "allow".to_string();
    notifications
        .apply_update(update, "all/test:listened")
        .unwrap();
    assert_eq!(waiting.snapshot().state, STATE_EXECUTED);
    assert_eq!(*response.borrow(), Some("allow".to_string()));

    // Executed notifications cannot be updated anymore.
    let mut again = waiting.snapshot();
    again.selected_action_id = "allow".to_string();
    assert!(notifications
        .apply_update(again, "all/test:listened")
        .is_err());

    // An update that marks the notification as executed externally.
    let handle = notifications.notify(notification("test:external")).unwrap();
    let mut update = handle.snapshot();
    update.state = STATE_EXECUTED.to_string();
    notifications
        .apply_update(update, "all/test:external")
        .unwrap();
    assert_eq!(handle.snapshot().state, STATE_EXECUTED);

    // Unknown keys create new notifications.
    let created = notifications
        .apply_update(notification("test:new"), "all/test:new")
        .unwrap();
    assert_eq!(created.state, STATE_ACTIVE);

    // Keys outside of `all/` are invalid.
    assert!(notifications
        .apply_update(notification("test:bad"), "other/test")
        .is_err());
}

#[tokio::test]
async fn database_serves_notifications() {
    let notifications = registry();
    let databases = Arc::new(Databases::without_disk());
    databases
        .register_injected("notifications", "Notifications")
        .unwrap();
    let controller = databases
        .inject(
            "notifications",
            Box::new(NotificationsStorage::new(Arc::clone(&notifications))),
        )
        .unwrap();
    notifications.set_db_controller(controller);

    let internal = databases.internal_interface();
    let mut subscription = internal.subscribe(Query::new("notifications:")).unwrap();

    let _handle = notifications
        .notify_info("test:db", "Title", "Message")
        .unwrap();
    let record = subscription.try_recv().expect("notification update");
    assert_eq!(record.key.to_string(), "notifications:all/test:db");
    let json: serde_json::Value = serde_json::from_slice(&record.data).unwrap();
    assert_eq!(json["EventID"], "test:db");
    assert_eq!(json["State"], "active");

    // Reading through the database.
    let record = internal.get("notifications:all/test:db").unwrap();
    assert!(!record.data.is_empty());
    assert!(internal.get("notifications:other/test:db").is_err());

    // Queries skip deleted notifications.
    let second = notifications
        .notify_info("test:db-2", "Title", "Message")
        .unwrap();
    let query = Query::new("notifications:all/");
    let records = internal.query(&query).unwrap();
    assert!(records.iter().any(|record| record.key.key == "all/test:db"));
    notifications.delete("test:db");
    let records = internal.query(&query).unwrap();
    assert!(!records.iter().any(|record| record.key.key == "all/test:db"));

    // Writing an update through the database. Without a listener the action is
    // only marked as responded, like in Go.
    let mut record = internal.get("notifications:all/test:db-2").unwrap();
    let mut update = second.snapshot();
    update.selected_action_id = "ack".to_string();
    record.data = pm_dsd::gojson::to_vec(&update).unwrap();
    internal.put(record).unwrap();
    assert_eq!(second.snapshot().state, STATE_RESPONDED);
    assert_eq!(second.snapshot().selected_action_id, "ack");

    // A listener makes the action executed.
    let third = notifications
        .notify_info("test:db-3", "Title", "Message")
        .unwrap();
    let response = third.response();
    let mut record = internal.get("notifications:all/test:db-3").unwrap();
    let mut update = third.snapshot();
    update.selected_action_id = "ack".to_string();
    record.data = pm_dsd::gojson::to_vec(&update).unwrap();
    internal.put(record).unwrap();
    assert_eq!(third.snapshot().state, STATE_EXECUTED);
    assert_eq!(*response.borrow(), Some("ack".to_string()));

    // Deleting through the database.
    internal.delete("notifications:all/test:db-2").unwrap();
    assert!(internal.get("notifications:all/test:db-2").is_err());
}

#[test]
fn config_option_is_registered() {
    let config = pm_config::Config::new();
    pm_notifications::register_options(&config).unwrap();
    assert!(config.get_as_bool(
        pm_notifications::CFG_USE_SYSTEM_NOTIFICATIONS_KEY,
        false
    )());
    config
        .set_config_option(
            pm_notifications::CFG_USE_SYSTEM_NOTIFICATIONS_KEY,
            json!(false),
        )
        .unwrap();
    assert!(!config.get_as_bool(
        pm_notifications::CFG_USE_SYSTEM_NOTIFICATIONS_KEY,
        true
    )());
    assert_eq!(
        config
            .option(pm_notifications::CFG_USE_SYSTEM_NOTIFICATIONS_KEY)
            .unwrap()
            .definition()
            .name,
        "Desktop Notifications"
    );
}
