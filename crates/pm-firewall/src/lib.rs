//! Port of the Go `service/firewall` verdict logic: `decideOnConnection`, the
//! decider chain, bypass prevention, the pre-authenticated port registry and
//! the DGA heuristics.
//!
//! # Compatibility notes
//!
//! * Go globals (own PID, netenv state, resolver state, custom lists, unbreak
//!   lists, prompts) are injected through [`FirewallEnv`].
//! * The bypass responder (`nsutil.Responder`, which Go keeps in the reason
//!   context) is returned in [`Decision::responder`]; the reason context stays
//!   unset. [`Decision::into_filter_outcome`] converts it for the nameserver's
//!   `FirewallHook`.
//! * Packet handling lives in [`packet`]. DNS response filtering, prompts,
//!   split tunnel and SPN routing are not part of this crate; the packet
//!   handler reaches them through hooks.

mod bypass;
mod deciders;
mod dga;
mod env;
mod levenshtein;
mod master;
pub mod packet;
mod preauth;

pub use bypass::{looks_like_outgoing_dns_request, prevent_bypassing, DnsResponder};
pub use deciders::*;
pub use dga::{lms_score, lms_score_of_domain};
pub use env::{CustomLists, FirewallEnv, LocalNetwork, MulticastRequest, NoCustomLists};
pub use master::{decide_on_connection, run_deciders, Decision};
pub use preauth::{protocol_from_network, PreAuthPorts};

/// Whether the process is, or belongs to, the system resolver.
pub fn is_system_resolver(process: &pm_process::Process) -> bool {
    process.primary_profile_id
        == pm_profile::make_scoped_id(
            pm_profile::SOURCE_LOCAL,
            pm_profile::SYSTEM_RESOLVER_PROFILE_ID,
        )
}
