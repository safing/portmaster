//! Finding processes for connections.

use pm_netstate::UNDEFINED_PROCESS_ID as SOCKET_UNDEFINED_PROCESS_ID;
use pm_netstate::{ip_scope, IpProtocol, IpScope, LookupError, NetState, PacketInfo};

use crate::cache::ProcessCache;
use crate::{Process, UNDEFINED_PROCESS_ID, UNIDENTIFIED_PROCESS_ID, UNSOLICITED_PROCESS_ID};

/// Returns the PID of the process that owns the described connection, with the
/// same fallbacks as Go `GetPidOfConnection`.
pub fn get_pid_of_connection(
    cache: &ProcessCache,
    state: &NetState,
    packet: &mut PacketInfo,
    fast: bool,
) -> (i32, bool, Option<LookupError>) {
    if !cache.enabled() {
        return (UNIDENTIFIED_PROCESS_ID, packet.inbound, None);
    }

    // Use the fast search for inbound packets: listening sockets exist already.
    let fast_search = fast || packet.inbound;
    let mut connection_inbound = packet.inbound;
    let mut error = None;

    let mut pid = if packet.pid == SOCKET_UNDEFINED_PROCESS_ID {
        match state.lookup(packet, fast_search) {
            Ok(result) => {
                connection_inbound = result.inbound;
                result.pid
            }
            Err(lookup_error) => {
                error = Some(lookup_error);
                UNDEFINED_PROCESS_ID
            }
        }
    } else {
        // eBPF or a kernel extension already assigned the PID.
        packet.pid
    };

    if pid == UNDEFINED_PROCESS_ID {
        pid = match connection_inbound {
            false => UNIDENTIFIED_PROCESS_ID,
            // Localhost and ICMP connections are always unidentified.
            true if ip_scope(packet.dst) == IpScope::HostLocal => UNIDENTIFIED_PROCESS_ID,
            true if packet.protocol == IpProtocol::Icmp => UNIDENTIFIED_PROCESS_ID,
            // All other unattributed inbound connections are unsolicited.
            true => UNSOLICITED_PROCESS_ID,
        };
    }

    (pid, connection_inbound, error)
}

/// Returns the process of a connection, like Go `GetProcessWithProfile`.
pub fn get_process_of_connection(
    cache: &std::sync::Arc<ProcessCache>,
    state: &NetState,
    packet: &mut PacketInfo,
    fast: bool,
) -> std::result::Result<std::sync::Arc<Process>, crate::cache::Error> {
    let (pid, _inbound, _error) = get_pid_of_connection(cache, state, packet, fast);
    cache.get_or_find(pid)
}
