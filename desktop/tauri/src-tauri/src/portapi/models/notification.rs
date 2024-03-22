use serde::*;

#[derive(Serialize, Deserialize, Debug, PartialEq)]
pub struct Notification {
    #[serde(rename = "EventID")]
    pub event_id: String,

    #[serde(rename = "GUID")]
    pub guid: String,

    #[serde(rename = "Type")]
    pub notification_type: NotificationType,

    #[serde(rename = "Message")]
    pub message: String,

    #[serde(rename = "Title")]
    pub title: String,
    #[serde(rename = "Category")]
    pub category: String,

    #[serde(rename = "EventData")]
    pub data: serde_json::Value,

    #[serde(rename = "Expires")]
    pub expires: u64,

    #[serde(rename = "State")]
    pub state: String,

    #[serde(rename = "AvailableActions")]
    pub actions: Vec<Action>,

    #[serde(rename = "SelectedActionID")]
    pub selected_action_id: String,

    #[serde(rename = "ShowOnSystem")]
    pub show_on_system: bool,
}

#[derive(Serialize, Deserialize, Debug, PartialEq)]
pub struct Action {
    #[serde(rename = "ID")]
    pub id: String,

    #[serde(rename = "Text")]
    pub text: String,

    #[serde(rename = "Type")]
    pub action_type: String,

    #[serde(rename = "Payload")]
    pub payload: serde_json::Value,
}

#[derive(Serialize, Deserialize, Debug, PartialEq)]
pub struct NotificationType(i32);

#[allow(dead_code)]
pub const INFO: NotificationType = NotificationType(0);

#[allow(dead_code)]
pub const WARN: NotificationType = NotificationType(1);

#[allow(dead_code)]
pub const PROMPT: NotificationType = NotificationType(2);

#[allow(dead_code)]
pub const ERROR: NotificationType = NotificationType(3);

