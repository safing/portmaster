use serde::{Serialize, Deserialize};

/// SystemResult defines the "success" codes when querying or starting
/// a system service. 
#[derive(Serialize, Deserialize, Debug, PartialEq)]
pub enum StatusResult {
    // The requested system service is installed and currently running.
    Running,

    // The requested system service is installed but currently stopped.
    Stopped,

    // NotFound is returned when the system service (systemd unit for linux)
    // has not been found and the system and likely means the Portmaster installtion
    // is broken all together. 
    NotFound,
}

impl std::fmt::Display for StatusResult {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            StatusResult::Running => write!(f, "running"),
            StatusResult::Stopped => write!(f, "stopped"),
            StatusResult::NotFound => write!(f, "not installed")
        }
    }
}
