//! Port of Go `service/firewall/interception` for Linux (nfqueue backend).
//!
//! # Platform support
//!
//! Packet interception uses Linux netlink (nfqueue and ctnetlink) and is only
//! available on Linux. On other platforms the packet parsing, mark and
//! iptables-rule modules remain available, while [`conntrack`] and [`queue`]
//! provide inert fallbacks: no packets are intercepted and the conntrack
//! operations report an error. The Windows kernel-extension backend from Go
//! (`windowskext`/`windowskext2`) is not ported.

pub mod iptables;
pub mod marks;
pub mod packet;

#[cfg(target_os = "linux")]
pub mod conntrack;
#[cfg(not(target_os = "linux"))]
#[path = "conntrack_unsupported.rs"]
pub mod conntrack;

#[cfg(target_os = "linux")]
pub mod queue;
#[cfg(not(target_os = "linux"))]
#[path = "queue_unsupported.rs"]
pub mod queue;

pub use marks::{Mark, Verdict};
pub use packet::{icmp_type, parse_layer3, ParseError};
