use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum StateType {
    #[serde(rename = "")]
    Undefined,
    #[serde(rename = "hint")]
    Hint,
    #[serde(rename = "warning")]
    Warning,
    #[serde(rename = "error")]
    Error,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct State {
    #[serde(rename = "ID")]
    pub id: String,
    #[serde(rename = "Name")]
    pub name: String,
    #[serde(rename = "Message")]
    pub message: Option<String>,
    #[serde(rename = "Type")]
    pub state_type: Option<StateType>,
    #[serde(rename = "Time")]
    pub time: Option<String>, // time.Time serialized by GoLang
    #[serde(rename = "Data")]
    pub data: Option<serde_json::Value>, // any type
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StateUpdate {
    #[serde(rename = "Module")]
    pub module: String,
    #[serde(rename = "States")]
    pub states: Option<Vec<State>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorstState {
    #[serde(rename = "Module")]
    pub module: String,
    #[serde(flatten)]
    pub state: State,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct SystemStatus {
    #[serde(rename = "Modules")]
    pub modules: Vec<StateUpdate>,
    #[serde(rename = "WorstState")]
    pub worst_state: Option<WorstState>,

    // add more fields when needed
    // ...
}

// PauseInfo represents pause status data from "control:paused" state in Control module
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct PauseInfo {
    #[serde(rename = "Interception")]
    pub interception: bool,
    #[serde(rename = "SPN")]  
    pub spn: bool,
    #[serde(rename = "TillTime")]
    pub till_time: String, // time.Time serialized as string by GoLang
}


impl SystemStatus {
    pub fn get_module_state(&self, module_name: &str, state_id: &str) -> Option<&State> {
        if let Some(module) = self.modules.iter().find(|m| m.module == module_name) {
            if let Some(states) = &module.states {
                return states.iter().find(|s| s.id == state_id);
            }
        }
        None
    }
    
}