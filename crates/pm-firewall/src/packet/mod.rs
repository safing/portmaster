//! Port of Go `service/firewall/packet_handler.go`: the glue between the
//! intercepted packets and the connection decisions.
//!
//! # Compatibility notes
//!
//! * Dispatch follows Go: one dispatcher ([`PacketHandler::dispatch`], single
//!   caller) finds or creates the connection and installs its handler state
//!   before anyone else can see it, then queues the packet (100 per
//!   connection, a full queue drops the packet). A drain job per connection
//!   runs the handler chain under the connection lock on an [`Executor`], in
//!   arrival order, parallel to other connections. After the chain finished
//!   the dispatcher applies the default handler itself (taking the
//!   connection lock; it falls back to the executor if the lock is busy).
//!   Handler state exists only while a chain is active;
//!   [`PacketHandler::prune_handlers`] removes the state of connections that
//!   left the store while still gathering data.
//! * [`QueuedPacket::set_verdict`] replaces Go's per-verdict methods; the
//!   `QueuedPacket` of pm-interception is wrapped by [`NfqPacket`] here, so
//!   pm-interception stays independent of the firewall.
//! * The packet reordering of the Go worker (waiting for info-only packets)
//!   is not ported; info-only packets only leave a PID hint (two generations
//!   like Go, kept in this crate instead of pm-network).
//! * An ICMP packet without ICMP layer is handled like in Go ([`IcmpType`]).
//! * Not ported: SPN tunnels (hooks in [`PacketEnv`], default no-op), the
//!   external verdict handler (hook), the stat logger (use
//!   [`PacketHandler::take_stats`]), inspectors (none exist in Go).
//! * Verdict reset deletes the conntrack entry once per changed IP
//!   connection; Go deletes it twice for IP connections and once for DNS
//!   connections, where it cannot match anything.

mod bandwidth;
mod dispatch;
mod dns;
mod env;
mod handler;
mod pid_hints;
mod queued;
mod reset;

pub use bandwidth::{BandwidthMethod, BandwidthUpdate};
pub use env::{
    BandwidthSink, ConnectionTuple, ConntrackControl, ExternalVerdict, LayeredProfileLookup,
    PacketEnv, SystemConntrack,
};
pub use dispatch::{Executor, PacketHandler};
pub use handler::{MatcherAlreadySet, PacketStats};
pub use queued::{IcmpType, NfqPacket, QueuedPacket, VerdictError};
