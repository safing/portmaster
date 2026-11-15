//! Module lifecycle manager and event subsystem for Portmaster.

pub mod events;
pub mod group;
pub mod manager;
pub mod module;

pub use events::{CallbackResult, EventMgr};
pub use group::Group;
pub use manager::{Manager, WorkerResult};
pub use module::{Module, ModuleState};
