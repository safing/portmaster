//! Port of the Go `service/firewall` verdict logic: `decideOnConnection`, the
//! decider chain, bypass prevention, the pre-authenticated port registry and
//! the DGA heuristics.
//!
//! # Compatibility notes
//!
//! * Go globals (own PID, netenv state, resolver state, custom lists, unbreak
//!   lists, prompts) are injected through [`FirewallEnv`].
//! * The bypass responder (`nsutil.Responder`) is returned by
//!   [`prevent_bypassing`] but not stored in the connection's reason context
//!   (Go serializes it to `null` anyway).
//! * The local profile's endpoint rules for system resolver DNS requests are
//!   parsed from the profile config here.
//! * Packet handling, DNS response filtering, prompts, split tunnel and SPN
//!   routing are not part of this crate.

mod bypass;
mod deciders;
mod dga;
mod env;
mod levenshtein;
mod master;
mod preauth;

pub use bypass::{looks_like_outgoing_dns_request, prevent_bypassing, BypassResponder};
pub use deciders::*;
pub use dga::{lms_score, lms_score_of_domain};
pub use env::{CustomLists, FirewallEnv, LocalNetwork, MulticastRequest, NoCustomLists};
pub use master::{decide_on_connection, run_deciders};
pub use preauth::{protocol_from_network, PreAuthPorts};

/// Whether the process is, or belongs to, the system resolver.
pub fn is_system_resolver(process: &pm_process::Process) -> bool {
    process.primary_profile_id
        == pm_profile::make_scoped_id(
            pm_profile::SOURCE_LOCAL,
            pm_profile::SYSTEM_RESOLVER_PROFILE_ID,
        )
}
