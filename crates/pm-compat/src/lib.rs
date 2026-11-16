//! Port of the Linux part of Go `service/compat`.
//!
//! This crate contains the compatibility module of Portmaster: the periodic
//! system-integration / DNS self-check, the conflict and compatibility
//! notifications, the iptables-chain debug section and the hooks that the
//! firewall calls when it observes a self-check packet or query.
//!
//! Only the Linux implementation is ported. The Windows WFP state machine
//! (`wfpstate.go`) and the Windows-specific debug code are deliberately not
//! ported.
//!
//! Network and iptables access lives behind the [`selfcheck::Probe`] and
//! [`iptables::ChainLister`] traits so that the decision logic can be tested
//! without root privileges or a live system. [`selfcheck::RealProbe`] and
//! [`iptables::SystemChainLister`] are the production adapters.

pub mod debug;
pub mod iptables;
pub mod module;
pub mod notify;
pub mod selfcheck;

pub use module::{
    Compat, CompatDeps, CLEAN_NOTIFY_THRESHOLD_REPEAT, SELF_CHECK_FAIL_THRESHOLD,
    SELF_CHECK_REPEAT, SELF_CHECK_RETRY_AFTER,
};
pub use notify::{
    AppIssueKind, NotifyThreshold, SystemIssueKind, CFG_OPTION_DNS_QUERY_INTERCEPTION_KEY,
    NOTIFY_THRESHOLD_MIN_INCIDENTS, NOTIFY_THRESHOLD_RESET_AFTER,
};
pub use selfcheck::{
    selfcheck, DnsResult, PacketSendError, Probe, RealProbe, SelfcheckConfig, SelfcheckError,
    SelfcheckErrorKind, DNS_CHECK_INTERNAL_DOMAIN_SCOPE, SELF_CHECK_PAYLOAD,
    SYSTEM_INTEGRATION_CHECK_DST_IP, SYSTEM_INTEGRATION_CHECK_PROTOCOL,
};
