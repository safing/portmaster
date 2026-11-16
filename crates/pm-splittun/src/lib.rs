//! Port of the Go `service/splittun` module.
//!
//! The module registers the `splittun/enable` option, keeps a registry of
//! connections that the firewall rerouted into the split tunnel and runs the
//! TCP/UDP proxies those connections arrive at.
//!
//! # Compatibility notes
//!
//! * Interface lookups and the IPv6 availability check are injected through
//!   [`InterfaceEnv`]; the netenv module fills this in.
//! * The proxy lifecycle ([`Proxies`]) is driven by the caller instead of a
//!   module manager worker.
//! * Egress tracking of the Go proxy session cache is not ported yet;
//!   [`RequestQueue`] only covers the pending-request side.

mod config;
mod proxies;
mod requests;

pub use config::{register_options, CFG_OPTION_SPLIT_TUN_ENABLE_KEY};
pub use proxies::Proxies;
pub use requests::{
    BestPhysicalDefaultInterfaces, InterfaceEnv, InterfaceInfo, PendingRequest, RequestQueue,
    PENDING_REQUEST_TTL, SPLIT_TUN_PORT,
};
