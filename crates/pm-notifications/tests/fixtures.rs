//! Compatibility test for the JSON form of a notification.

use pm_notifications::{
    Action, Notification, ACTION_TYPE_NONE, ACTION_TYPE_OPEN_SETTING, ACTION_TYPE_WEBHOOK,
    ACTION_VISIBILITY_DEFAULT, ACTION_VISIBILITY_DETAILED, ACTION_VISIBILITY_IN_APP_ONLY,
    STATE_ACTIVE,
};
use serde_json::{json, Value};

#[test]
fn notification_json_matches_go() {
    let path = concat!(
        env!("CARGO_MANIFEST_DIR"),
        "/../fixtures/m0/notifications.json"
    );
    let fixture: Value = serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap();
    let expected = fixture["notification"].as_str().unwrap();

    let notification = Notification {
        event_id: "module:event-1".to_string(),
        guid: "9f8b7c6d-5e4f-3a2b-1c0d-9e8f7a6b5c4d".to_string(),
        type_: pm_notifications::TYPE_PROMPT,
        title: "Test Notification".to_string(),
        category: "Test".to_string(),
        message: "Do you want to continue?".to_string(),
        show_on_system: true,
        // Go sorts map keys; keep the same order for the byte comparison.
        event_data: json!({"count": 2, "process": "curl"}),
        expires: 1_700_000_900,
        state: STATE_ACTIVE.to_string(),
        available_actions: vec![
            Action::new("allow", "Allow")
                .with_type(
                    ACTION_TYPE_OPEN_SETTING,
                    serde_json::to_value(pm_notifications::ActionTypeOpenSettingPayload {
                        key: "filter/defaultAction".to_string(),
                        profile: "local/abc".to_string(),
                    })
                    .unwrap(),
                )
                .with_visibility(ACTION_VISIBILITY_DEFAULT),
            Action::new("deny", "Deny")
                .with_type(ACTION_TYPE_NONE, Value::Null)
                .with_visibility(ACTION_VISIBILITY_DETAILED),
            Action::new("hook", "Call")
                .with_type(
                    ACTION_TYPE_WEBHOOK,
                    serde_json::to_value(pm_notifications::ActionTypeWebhookPayload {
                        method: "POST".to_string(),
                        url: "/api/v1/test".to_string(),
                        payload: json!({"a": 1}),
                        result_action: "display".to_string(),
                    })
                    .unwrap(),
                )
                .with_visibility(ACTION_VISIBILITY_IN_APP_ONLY),
        ],
        selected_action_id: "allow".to_string(),
    };

    let serialized = String::from_utf8(pm_dsd::gojson::to_vec(&notification).unwrap()).unwrap();
    assert_eq!(serialized, expected);

    // The notification round-trips through serde.
    let decoded: Notification = serde_json::from_str(expected).unwrap();
    assert_eq!(decoded.type_, pm_notifications::TYPE_PROMPT);
    assert_eq!(decoded.available_actions.len(), 3);
    assert_eq!(decoded.key(), "notifications:all/module:event-1");
    assert_eq!(decoded.action("hook").unwrap().type_, ACTION_TYPE_WEBHOOK);
}
