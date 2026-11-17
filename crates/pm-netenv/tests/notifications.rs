//! Tests for the network environment notification events.

use std::sync::{Arc, Mutex};

use pm_netenv::{
    captive_portal_event, notification_change, online_status_event, CaptivePortal, NetEnv,
    NotificationChange, NotificationEvent, NotificationSink, OnlineStatus, CAPTIVE_PORTAL_EVENT_ID,
    CAPTIVE_PORTAL_MESSAGE, CAPTIVE_PORTAL_TITLE, IGNORE_ACTION_ID, LIMITED_EVENT_ID,
    OFFLINE_EVENT_ID, OPEN_PORTAL_ACTION_TYPE,
};

#[derive(Default)]
struct RecordingSink {
    changes: Mutex<Vec<NotificationChange>>,
}

impl NotificationSink for RecordingSink {
    fn handle(&self, change: NotificationChange) {
        self.changes.lock().unwrap().push(change);
    }
}

#[test]
fn captive_portal_event_uses_go_strings_and_actions() {
    let portal = CaptivePortal::from_url("http://192.0.2.1/login");
    let event: NotificationEvent = captive_portal_event(&portal);
    assert_eq!(event.event_id, CAPTIVE_PORTAL_EVENT_ID);
    assert_eq!(event.title, CAPTIVE_PORTAL_TITLE);
    assert_eq!(event.message, CAPTIVE_PORTAL_MESSAGE);
    assert!(event.show_on_system);
    assert_eq!(event.available_actions.len(), 2);
    assert_eq!(event.available_actions[0].type_, OPEN_PORTAL_ACTION_TYPE);
    assert_eq!(event.available_actions[1].id, IGNORE_ACTION_ID);
}

#[test]
fn status_events_are_only_emitted_for_offline_and_limited() {
    assert_eq!(
        online_status_event(OnlineStatus::Offline).unwrap().event_id,
        OFFLINE_EVENT_ID
    );
    assert_eq!(
        online_status_event(OnlineStatus::Limited).unwrap().event_id,
        LIMITED_EVENT_ID
    );
    assert!(online_status_event(OnlineStatus::Portal).is_none());
    assert!(online_status_event(OnlineStatus::Online).is_none());
    assert!(online_status_event(OnlineStatus::Unknown).is_none());
}

#[test]
fn notification_changes_follow_go_lifecycle() {
    let portal = CaptivePortal::from_url("http://portal.example/login");

    let first = notification_change(OnlineStatus::Offline, None, false, None);
    assert_eq!(first.notify.len(), 1);
    assert_eq!(first.notify[0].event_id, OFFLINE_EVENT_ID);

    // An unchanged status notification is not re-sent.
    let second = notification_change(OnlineStatus::Offline, None, false, Some(OFFLINE_EVENT_ID));
    assert!(second.notify.is_empty());

    // Switching Offline -> Limited deletes the old event and sends a new one.
    let switched = notification_change(OnlineStatus::Limited, None, false, Some(OFFLINE_EVENT_ID));
    assert!(switched.delete_online_status);
    assert_eq!(switched.notify[0].event_id, LIMITED_EVENT_ID);

    // Coming online deletes the status notification but sends no new one.
    let third = notification_change(OnlineStatus::Online, None, false, Some(LIMITED_EVENT_ID));
    assert!(third.delete_online_status);
    assert!(!third.delete_captive_portal);

    // A new portal triggers exactly one portal event.
    let fourth = notification_change(OnlineStatus::Portal, Some(&portal), false, None);
    assert_eq!(fourth.notify.len(), 1);
    assert_eq!(fourth.notify[0].event_id, CAPTIVE_PORTAL_EVENT_ID);

    let fifth = notification_change(OnlineStatus::Online, None, true, None);
    assert!(fifth.delete_captive_portal);
}

#[test]
fn netenv_emits_notification_events_on_status_changes() {
    let sink = Arc::new(RecordingSink::default());
    let netenv = NetEnv::with_notification_sink(sink.clone());

    netenv.update_online_status(OnlineStatus::Offline, None);
    let changes = sink.changes.lock().unwrap();
    assert_eq!(changes.len(), 1);
    assert_eq!(changes[0].notify[0].event_id, OFFLINE_EVENT_ID);
    drop(changes);

    netenv.update_online_status(OnlineStatus::Online, None);
    let changes = sink.changes.lock().unwrap();
    assert_eq!(changes.len(), 2);
    assert!(changes[1].delete_online_status);
}

#[test]
fn portal_deletion_is_notified_even_when_status_does_not_change() {
    let sink = Arc::new(RecordingSink::default());
    let netenv = NetEnv::with_notification_sink(sink.clone());

    let portal = CaptivePortal::from_url("http://portal.example/login");
    netenv.update_online_status(OnlineStatus::Portal, Some(portal));
    assert!(netenv.captive_portal().is_some());

    // The portal disappears but the status number stays `Portal`.
    netenv.update_online_status(OnlineStatus::Portal, None);
    let changes = sink.changes.lock().unwrap();
    assert_eq!(changes.len(), 2, "a notification change is still emitted");
    assert!(changes[1].delete_captive_portal);
    assert!(netenv.captive_portal().is_none());
}

#[test]
fn global_spn_and_dns_flags_are_independent() {
    pm_netenv::set_connected_to_spn(true);
    assert!(pm_netenv::is_connected_to_spn());
    assert!(!pm_netenv::is_connected_to_dns());
    pm_netenv::set_connected_to_spn(false);
    pm_netenv::set_connected_to_dns(true);
    assert!(pm_netenv::is_connected_to_dns());
    pm_netenv::set_connected_to_dns(false);
}
