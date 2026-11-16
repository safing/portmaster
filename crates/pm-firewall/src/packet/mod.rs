//! Port of Go `service/firewall/packet_handler.go`: the glue between the
//! intercepted packets and the connection decisions.
//!
//! # Compatibility notes
//!
//! * Go queues the packets of a connection into a worker that runs the
//!   connection's firewall handler under the connection lock. The port runs
//!   the handler synchronously on the calling thread under the same lock
//!   ([`PacketHandler::handle_packet`]); packets of one connection are
//!   serialized by that lock. The handler of a connection is kept in the
//!   packet handler, not in the connection; [`PacketHandler::prune_handlers`]
//!   drops it after the connection left the store. Call the handler from
//!   blocking workers: gathering connection info does socket table lookups.
//! * [`QueuedPacket::set_verdict`] replaces Go's per-verdict methods; the
//!   `QueuedPacket` of pm-interception is wrapped by [`NfqPacket`] here, so
//!   pm-interception stays independent of the firewall.
//! * The packet reordering of the Go worker (waiting for info-only packets)
//!   is not ported; info-only packets only leave a PID hint.
//! * Not ported: SPN tunnels (hooks in [`PacketEnv`], default no-op), the
//!   external verdict handler (hook), the stat logger (use
//!   [`PacketHandler::take_stats`]), inspectors (none exist in Go).
//! * Verdict reset deletes the conntrack entry once per changed IP
//!   connection; Go deletes it twice for IP connections and once for DNS
//!   connections, where it cannot match anything.

mod bandwidth;
mod dns;
mod env;
mod handler;
mod queued;
mod reset;

pub use bandwidth::{BandwidthMethod, BandwidthUpdate};
pub use env::{
    BandwidthSink, ConnectionTuple, ConntrackControl, ExternalVerdict, LayeredProfileLookup,
    PacketEnv, SystemConntrack,
};
pub use handler::{MatcherAlreadySet, PacketHandler, PacketStats};
pub use queued::{NfqPacket, QueuedPacket, VerdictError};
