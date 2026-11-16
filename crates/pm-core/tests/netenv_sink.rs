//! Tests for the netenv notification bridge in the core.

use std::sync::Arc;

use pm_core::netenv_sink::NetEnvNotificationSink;
use pm_netenv::{
    notification_change, NotificationSink, OnlineStatus, OFFLINE_EVENT_ID, OFFLINE_TITLE,
};
use pm_notifications::Notifications;

#[test]
fn sends_and_deletes_status_notifications() {
    let notifications = Arc::new(Notifications::new());
    let sink = NetEnvNotificationSink::new(Arc::clone(&notifications));

    let change = notification_change(OnlineStatus::Offline, None, false, None);
    sink.handle(change);

    let notification = notifications
        .get(OFFLINE_EVENT_ID)
        .expect("offline notification")
        .snapshot();
    assert_eq!(notification.event_id, OFFLINE_EVENT_ID);
    assert_eq!(notification.title, OFFLINE_TITLE);
    assert_eq!(
        notification.message,
        "Portmaster did not detect any network connectivity."
    );

    // Going online removes the offline notification.
    let change = notification_change(
        OnlineStatus::Online,
        None,
        false,
        Some(OFFLINE_EVENT_ID),
    );
    sink.handle(change);
    assert!(notifications.get(OFFLINE_EVENT_ID).is_none());
}

#[test]
fn captive_portal_actions_are_forwarded() {
    let notifications = Arc::new(Notifications::new());
    let sink = NetEnvNotificationSink::new(Arc::clone(&notifications));
    let portal = pm_netenv::CaptivePortal::new(
        "http://portal.example/login",
        "portal.example",
        None,
    );
    let change = pm_netenv::notification_change(OnlineStatus::Portal, Some(&portal), false, None);
    sink.handle(change);

    let notification = notifications
        .get(pm_netenv::CAPTIVE_PORTAL_EVENT_ID)
        .expect("portal notification")
        .snapshot();
    assert_eq!(notification.title, pm_netenv::CAPTIVE_PORTAL_TITLE);
    assert!(notification.show_on_system);
    assert_eq!(notification.available_actions.len(), 2);
    assert_eq!(notification.available_actions[0].text, "Open Portal");
    assert_eq!(
        notification.available_actions[0].type_,
        pm_notifications::ACTION_TYPE_OPEN_URL
    );
    assert_eq!(
        notification.available_actions[0].payload,
        serde_json::json!("http://portal.example/login")
    );
    assert_eq!(notification.available_actions[1].id, "ack");

    let change = pm_netenv::notification_change(
        OnlineStatus::Online,
        None,
        true,
        Some(pm_netenv::CAPTIVE_PORTAL_EVENT_ID),
    );
    sink.handle(change);
    assert!(notifications
        .get(pm_netenv::CAPTIVE_PORTAL_EVENT_ID)
        .is_none());
}
