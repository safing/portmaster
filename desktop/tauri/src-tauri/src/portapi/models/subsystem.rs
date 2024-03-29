#![allow(dead_code)]
use serde::*;

#[derive(Serialize, Deserialize, Debug, PartialEq, Clone)]
pub struct ModuleStatus {
    #[serde(rename = "Name")]
    pub name: String,

    #[serde(rename = "Enabled")]
    pub enabled: bool,

    #[serde(rename = "Status")]
    pub status: u8,

    #[serde(rename = "FailureStatus")]
    pub failure_status: u8,

    #[serde(rename = "FailureID")]
    pub failure_id: String,

    #[serde(rename = "FailureMsg")]
    pub failure_msg: String,
}

#[derive(Serialize, Deserialize, Debug, PartialEq, Clone)]
pub struct Subsystem {
    #[serde(rename = "ID")]
    pub id: String,

    #[serde(rename = "Name")]
    pub name: String,

    #[serde(rename = "Description")]
    pub description: String,

    #[serde(rename = "Modules")]
    pub module_status: Vec<ModuleStatus>,

    #[serde(rename = "FailureStatus")]
    pub failure_status: u8,
}
pub const FAILURE_NONE: u8 = 0;
pub const FAILURE_HINT: u8 = 1;
pub const FAILURE_WARNING: u8 = 2;
pub const FAILURE_ERROR: u8 = 3;
