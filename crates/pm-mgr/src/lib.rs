//! Module lifecycle manager and event subsystem for Portmaster.

pub mod events;
pub mod group;
pub mod manager;
pub mod module;
pub mod states;

pub use events::{CallbackResult, EventMgr};
pub use group::Group;
pub use manager::{Manager, WorkerResult};
pub use module::{Module, ModuleState};
pub use states::{
    severity, State, StateMgr, StateType, StateUpdate, STATE_TYPE_ERROR, STATE_TYPE_HINT,
    STATE_TYPE_UNDEFINED, STATE_TYPE_WARNING,
};
