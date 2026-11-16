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
//! * Packet handling lives in [`packet`]; DNS response filtering is a hook.
//! * Prompts are here; the ask timeout and profile storage are injected through
//!   [`PromptEnv`].
//! * Split tunnel verdicts are here, but the proxy is injected through
//!   [`SplitTunnelEnv`].

mod bypass;
mod config;
mod custom_lists;
mod deciders;
pub mod dns_filter;
mod dga;
mod env;
mod levenshtein;
mod master;
pub mod packet;
mod preauth;
mod prompt;
mod split_tunnel;

pub use bypass::{looks_like_outgoing_dns_request, prevent_bypassing, DnsResponder};
pub use custom_lists::ServiceCustomLists;
pub use config::{
    register_options, CFG_OPTION_ASK_TIMEOUT_KEY, CFG_OPTION_ASK_WITH_SYSTEM_NOTIFICATIONS_KEY,
    CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY, CFG_OPTION_ENABLE_FILTER_KEY,
    CFG_OPTION_PERMANENT_VERDICTS_KEY,
};
pub use deciders::*;
pub use dga::{lms_score, lms_score_of_domain};
pub use env::{CustomLists, FirewallEnv, LocalNetwork, MulticastRequest, NoCustomLists};
pub use master::{decide_on_connection, run_deciders, Decision};
pub use packet::{PacketMetricRecord, PacketMetrics};
pub use preauth::{protocol_from_network, PreAuthPorts};
pub use prompt::{
    create_prompt, endpoint_rule, prompt, PromptEnv, PromptOutcome, PromptProfile, PromptTarget,
    ALLOW_DOMAIN_ALL, ALLOW_DOMAIN_DISTINCT, ALLOW_IP, ALLOW_SERVING_IP, BLOCK_DOMAIN_ALL,
    BLOCK_DOMAIN_DISTINCT, BLOCK_IP, BLOCK_SERVING_IP, CANCEL_PROMPT, DECISION_TIMEOUT_SECONDS,
    PROMPT_ID_PREFIX,
};
pub use split_tunnel::{
    check_split_tunneling, is_own_split_tunnel_proxy_connection, request_split_tunneling,
    SplitTunnelEnv,
};

/// Whether the process is, or belongs to, the system resolver.
pub fn is_system_resolver(process: &pm_process::Process) -> bool {
    process.primary_profile_id
        == pm_profile::make_scoped_id(
            pm_profile::SOURCE_LOCAL,
            pm_profile::SYSTEM_RESOLVER_PROFILE_ID,
        )
}
