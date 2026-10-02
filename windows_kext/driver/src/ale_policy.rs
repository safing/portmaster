use smoltcp::wire::IpProtocol;

use crate::connection::Direction;

/// Policy result after combining the network and transport injection handles.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) enum AleInjectionAction {
    Process,
    PermitSelfInjected,
    PermitOtherInjectedOutbound,
}

/// Combined view of one NBL queried against both driver injection handles.
///
/// WFP can report the same self-injected NBL as `InjectedByOther` relative to
/// the other handle. Callers must therefore test `self_injected` first.
#[derive(Clone, Copy)]
pub(crate) struct InjectionStatus {
    self_injected: bool,
    injected_by_other: bool,
}

impl InjectionStatus {
    pub(crate) fn new(
        network_self_injected: bool,
        transport_self_injected: bool,
        network_injected_by_other: bool,
        transport_injected_by_other: bool,
    ) -> Self {
        Self {
            self_injected: network_self_injected || transport_self_injected,
            injected_by_other: network_injected_by_other || transport_injected_by_other,
        }
    }

    pub(crate) fn is_self_injected(self) -> bool {
        self.self_injected
    }

    pub(crate) fn is_injected_by_other(self) -> bool {
        self.injected_by_other
    }
}

/// Returns whether an ALE indication has the signature of a final TCP
/// reauthorization racing endpoint closure.
pub(crate) fn can_reuse_ended_tcp_policy(reauthorize: bool, protocol: IpProtocol) -> bool {
    reauthorize && protocol == IpProtocol::Tcp
}

/// Selects the ALE loop guard without allowing an "other" result from one
/// handle to override proof that the NBL belongs to the other local handle.
pub(crate) fn classify_ale_injection(
    injection: InjectionStatus,
    protocol: IpProtocol,
    loopback: bool,
    connection_direction: Direction,
    packet_direction: Direction,
) -> AleInjectionAction {
    if injection.is_self_injected()
        && !self_injected_packet_needs_accept_authorization(
            protocol,
            loopback,
            connection_direction,
            packet_direction,
        )
    {
        return AleInjectionAction::PermitSelfInjected;
    }

    if injection.is_injected_by_other() && matches!(packet_direction, Direction::Outbound) {
        return AleInjectionAction::PermitOtherInjectedOutbound;
    }

    AleInjectionAction::Process
}

/// Returns whether an outbound synthetic flow belongs to any injector.
pub(crate) fn should_skip_injected_outbound_flow(
    outbound: bool,
    network_injected: bool,
    transport_injected: bool,
) -> bool {
    outbound && (network_injected || transport_injected)
}

/// Initial outbound TCP has no NBL; all other ALE packet-bearing paths may clone it.
pub(crate) fn should_capture_ale_packet(
    protocol: IpProtocol,
    packet_direction: Direction,
    reauthorize: bool,
) -> bool {
    protocol != IpProtocol::Tcp || !matches!(packet_direction, Direction::Outbound) || reauthorize
}

/// An inbound packet reauthorizing AUTH_CONNECT has no injectable IP header there.
pub(crate) fn should_skip_cross_direction_ale_clone(
    reauthorize: bool,
    connection_direction: Direction,
    packet_direction: Direction,
) -> bool {
    reauthorize
        && matches!(connection_direction, Direction::Outbound)
        && matches!(packet_direction, Direction::Inbound)
}

/// Returns whether the transport endpoint handle of a self-injected ALE
/// indication identifies the application's own socket.
///
/// Reinjection re-indicates a packet without any application send context, so WFP
/// reports one shared raw endpoint for everything the injector emits. Runtime
/// capture showed a single outbound handle offered for dozens of unrelated
/// connections, including different remote addresses, which made each following
/// connection look like a handle collision and left the borrowed handle in the
/// cache as an alias of the first tuple that claimed it.
///
/// Nothing is lost by ignoring it: the genuine authorization that created the
/// pended request already associated the real endpoint, and `ALE_FLOW_ESTABLISHED`
/// rebinds the established child handle. An inbound self-injected packet is
/// returned to a concrete receiving endpoint and keeps native identity.
pub(crate) fn self_injected_endpoint_identifies_socket(packet_direction: Direction) -> bool {
    matches!(packet_direction, Direction::Inbound)
}

/// Returns whether a self-injected ALE indication must still run the server-side
/// TCP/UDP receive/accept authorization path.
///
/// Network reinjection of the first outbound loopback packet can be the first NBL
/// seen by the listening endpoint. Every other self-injected indication keeps the
/// normal immediate-permit loop guard.
pub(crate) fn self_injected_packet_needs_accept_authorization(
    protocol: IpProtocol,
    loopback: bool,
    connection_direction: Direction,
    packet_direction: Direction,
) -> bool {
    matches!(protocol, IpProtocol::Tcp | IpProtocol::Udp)
        && loopback
        && matches!(connection_direction, Direction::Inbound)
        && matches!(packet_direction, Direction::Inbound)
}

#[cfg(test)]
mod tests {
    use super::{
        can_reuse_ended_tcp_policy, classify_ale_injection,
        self_injected_endpoint_identifies_socket, self_injected_packet_needs_accept_authorization,
        should_capture_ale_packet, should_skip_cross_direction_ale_clone,
        should_skip_injected_outbound_flow, AleInjectionAction, InjectionStatus,
    };
    use crate::connection::Direction;
    use smoltcp::wire::IpProtocol;

    #[test]
    fn only_inbound_self_injected_endpoint_identifies_a_socket() {
        // An outbound reinjection carries the injector's shared raw endpoint for
        // every tuple it emits, so it must never be associated regardless of
        // whether the traffic happens to be local.
        assert!(!self_injected_endpoint_identifies_socket(
            Direction::Outbound
        ));
        assert!(self_injected_endpoint_identifies_socket(Direction::Inbound));
    }

    #[test]
    fn ended_policy_is_used_only_for_tcp_reauthorization() {
        assert!(can_reuse_ended_tcp_policy(true, IpProtocol::Tcp));
        assert!(!can_reuse_ended_tcp_policy(false, IpProtocol::Tcp));
        assert!(!can_reuse_ended_tcp_policy(true, IpProtocol::Udp));
    }

    #[test]
    fn only_inbound_loopback_transport_needs_accept_authorization() {
        for protocol in [IpProtocol::Tcp, IpProtocol::Udp] {
            assert!(self_injected_packet_needs_accept_authorization(
                protocol,
                true,
                Direction::Inbound,
                Direction::Inbound,
            ));
        }
        assert!(!self_injected_packet_needs_accept_authorization(
            IpProtocol::Icmp,
            true,
            Direction::Inbound,
            Direction::Inbound,
        ));
        assert!(!self_injected_packet_needs_accept_authorization(
            IpProtocol::Tcp,
            false,
            Direction::Inbound,
            Direction::Inbound,
        ));
        assert!(!self_injected_packet_needs_accept_authorization(
            IpProtocol::Tcp,
            true,
            Direction::Outbound,
            Direction::Inbound,
        ));
        assert!(!self_injected_packet_needs_accept_authorization(
            IpProtocol::Tcp,
            true,
            Direction::Inbound,
            Direction::Outbound,
        ));
    }

    #[test]
    fn self_injection_bypasses_ale_from_either_injection_handle() {
        for (network_self, transport_self, network_other, transport_other) in [
            (true, false, false, false),
            (false, true, false, false),
            (true, false, false, true),
            // Observed for transport-reinjected ALE clones at the IP layer:
            // "other" relative to the network handle, "self" relative to transport.
            (false, true, true, false),
        ] {
            assert_eq!(
                classify_ale_injection(
                    InjectionStatus::new(
                        network_self,
                        transport_self,
                        network_other,
                        transport_other,
                    ),
                    IpProtocol::Tcp,
                    false,
                    Direction::Outbound,
                    Direction::Outbound,
                ),
                AleInjectionAction::PermitSelfInjected
            );
        }
    }

    #[test]
    fn self_injected_loopback_transport_still_authorizes_server_endpoint() {
        for injection in [
            InjectionStatus::new(true, false, false, false),
            InjectionStatus::new(false, true, true, false),
        ] {
            for protocol in [IpProtocol::Tcp, IpProtocol::Udp] {
                assert_eq!(
                    classify_ale_injection(
                        injection,
                        protocol,
                        true,
                        Direction::Inbound,
                        Direction::Inbound,
                    ),
                    AleInjectionAction::Process
                );
            }
        }
    }

    #[test]
    fn foreign_injection_bypasses_only_outbound_ale() {
        for (network_other, transport_other) in [(true, false), (false, true)] {
            assert_eq!(
                classify_ale_injection(
                    InjectionStatus::new(false, false, network_other, transport_other),
                    IpProtocol::Udp,
                    false,
                    Direction::Outbound,
                    Direction::Outbound,
                ),
                AleInjectionAction::PermitOtherInjectedOutbound
            );
            assert_eq!(
                classify_ale_injection(
                    InjectionStatus::new(false, false, network_other, transport_other),
                    IpProtocol::Udp,
                    false,
                    Direction::Inbound,
                    Direction::Inbound,
                ),
                AleInjectionAction::Process
            );
        }
    }

    #[test]
    fn non_injected_and_unknown_packets_follow_normal_ale_policy() {
        assert_eq!(
            classify_ale_injection(
                InjectionStatus::new(false, false, false, false),
                IpProtocol::Tcp,
                false,
                Direction::Outbound,
                Direction::Outbound,
            ),
            AleInjectionAction::Process
        );
    }

    #[test]
    fn flow_established_skips_any_injected_outbound_origin_only() {
        for network_injected in [false, true] {
            for transport_injected in [false, true] {
                let injected = network_injected || transport_injected;
                assert_eq!(
                    should_skip_injected_outbound_flow(true, network_injected, transport_injected),
                    injected
                );
                assert!(!should_skip_injected_outbound_flow(
                    false,
                    network_injected,
                    transport_injected
                ));
            }
        }
    }

    #[test]
    fn outbound_tcp_capture_depends_on_reauthorization() {
        assert!(!should_capture_ale_packet(
            IpProtocol::Tcp,
            Direction::Outbound,
            false
        ));
        assert!(should_capture_ale_packet(
            IpProtocol::Tcp,
            Direction::Outbound,
            true
        ));
        assert!(should_capture_ale_packet(
            IpProtocol::Tcp,
            Direction::Inbound,
            false
        ));
        assert!(should_capture_ale_packet(
            IpProtocol::Udp,
            Direction::Outbound,
            false
        ));
    }

    #[test]
    fn only_inbound_packet_on_reauthorized_outbound_flow_skips_clone() {
        assert!(should_skip_cross_direction_ale_clone(
            true,
            Direction::Outbound,
            Direction::Inbound
        ));
        assert!(!should_skip_cross_direction_ale_clone(
            false,
            Direction::Outbound,
            Direction::Inbound
        ));
        assert!(!should_skip_cross_direction_ale_clone(
            true,
            Direction::Inbound,
            Direction::Inbound
        ));
        assert!(!should_skip_cross_direction_ale_clone(
            true,
            Direction::Outbound,
            Direction::Outbound
        ));
    }
}
