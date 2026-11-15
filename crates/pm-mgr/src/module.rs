use std::fmt;

use crate::manager::Manager;

/// Module state in its lifecycle.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum ModuleState {
    #[default]
    Off,
    Starting,
    Running,
    Stopping,
    Invalid,
}

impl ModuleState {
    /// Returns the canonical string representation of the state.
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Off => "off",
            Self::Starting => "starting",
            Self::Running => "running",
            Self::Stopping => "stopping",
            Self::Invalid => "invalid",
        }
    }
}

impl fmt::Display for ModuleState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.as_str())
    }
}

/// A manage-able module component.
#[async_trait::async_trait]
pub trait Module: Send + Sync {
    /// Returns the name of the module.
    fn name(&self) -> &str;

    /// Starts the module.
    async fn start(&self, mgr: &Manager) -> anyhow::Result<()>;

    /// Stops the module.
    async fn stop(&self, mgr: &Manager) -> anyhow::Result<()>;
}
