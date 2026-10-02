use core::fmt;

use smoltcp::wire::{
    IpAddress, IpProtocol, Ipv4Packet, Ipv6Packet, IPV4_HEADER_LEN, IPV6_HEADER_LEN,
};

use crate::{
    common::{
        ICMPV4_CODE_DU_PORT_UNREACHABLE, ICMPV4_TYPE_DESTINATION_UNREACHABLE,
        ICMPV6_CODE_DU_PORT_UNREACHABLE, ICMPV6_TYPE_DESTINATION_UNREACHABLE,
    },
    connection::Direction,
    connection_map::Key,
    ipv6_packet::walk_ipv6_headers,
};

/// Prefix large enough for the IPv6 base header, the bounded extension-header
/// chain and every transport field inspected by the packet callout.
pub(crate) const MAX_PACKET_INSPECT_LEN: usize = 128;

const IPV4_MAX_HEADER_LEN: usize = 60;
const ICMP_HEADER_LEN: usize = 8;
const ICMP_ECHO_ID_END: usize = 6;
const TCP_FLAGS_OFFSET: usize = 13;
const TCP_RST_FLAG: u8 = 0x04;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(crate) enum PacketMetadataError {
    UnreadablePacket,
    UnresolvedIpv6Headers,
}

impl fmt::Display for PacketMetadataError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(match self {
            Self::UnreadablePacket => "failed to get net_buffer data",
            Self::UnresolvedIpv6Headers => "IPv6 extension header chain did not resolve",
        })
    }
}

#[derive(Clone, Copy)]
pub(crate) struct IcmpEcho {
    pub(crate) is_request: bool,
    pub(crate) identifier: u16,
}

#[derive(Clone, Copy)]
pub(crate) struct PacketMetadata {
    pub(crate) key: Key,
    pub(crate) icmp_echo: Option<IcmpEcho>,
    pub(crate) is_icmp_port_unreachable: bool,
    pub(crate) is_tcp_reset: bool,
}

#[derive(Clone, Copy)]
pub(crate) struct PacketInspection {
    pub(crate) is_fragment: bool,
    pub(crate) metadata: Result<PacketMetadata, PacketMetadataError>,
}

impl PacketInspection {
    pub(crate) const fn unreadable() -> Self {
        Self {
            is_fragment: false,
            metadata: Err(PacketMetadataError::UnreadablePacket),
        }
    }
}

/// Parses every field needed by the packet callout from one bounded prefix.
///
/// Fragment state is returned independently from the remaining metadata. This
/// preserves the packet-layer rule that an identifiable individual fragment is
/// permitted even when it is too short to produce a connection key.
pub(crate) fn inspect_packet(packet: &[u8], ipv6: bool, direction: Direction) -> PacketInspection {
    if ipv6 {
        inspect_ipv6_packet(packet, direction)
    } else {
        inspect_ipv4_packet(packet, direction)
    }
}

fn inspect_ipv4_packet(packet: &[u8], direction: Direction) -> PacketInspection {
    if packet.len() < IPV4_HEADER_LEN {
        return PacketInspection::unreadable();
    }

    let ip_packet = Ipv4Packet::new_unchecked(packet);
    let is_fragment = ip_packet.frag_offset() != 0 || ip_packet.more_frags();

    // Preserve the existing key contract: four bytes beyond the base header
    // must be readable even for a protocol that does not use ports.
    if packet.len() < IPV4_HEADER_LEN + 4 {
        return PacketInspection {
            is_fragment,
            metadata: Err(PacketMetadataError::UnreadablePacket),
        };
    }

    let protocol = ip_packet.next_header();
    let raw_transport_offset = ip_packet.header_len() as usize;
    let transport_offset = core::cmp::max(raw_transport_offset, IPV4_HEADER_LEN);
    let transport = packet.get(transport_offset..).unwrap_or_default();
    let (source_port, destination_port) = get_ports(transport, protocol);
    let key = build_key(
        direction,
        protocol,
        IpAddress::Ipv4(ip_packet.src_addr()),
        source_port,
        IpAddress::Ipv4(ip_packet.dst_addr()),
        destination_port,
    );

    // The old IPv4 echo reader required the complete eight-byte ICMP header
    // even though the identifier itself ends at byte six.
    let icmp_echo = if protocol == IpProtocol::Icmp && transport.len() >= ICMP_HEADER_LEN {
        get_icmp_echo(transport, false)
    } else {
        None
    };

    let outbound = matches!(direction, Direction::Outbound);
    let is_icmp_port_unreachable = protocol == IpProtocol::Icmp
        && ip_packet.version() == 4
        && is_port_unreachable(
            packet,
            raw_transport_offset,
            usize::from(ip_packet.total_len()),
            false,
        );
    let is_tcp_reset = outbound
        && protocol == IpProtocol::Tcp
        && (IPV4_HEADER_LEN..=IPV4_MAX_HEADER_LEN).contains(&raw_transport_offset)
        && has_tcp_reset(packet, raw_transport_offset);

    PacketInspection {
        is_fragment,
        metadata: Ok(PacketMetadata {
            key,
            icmp_echo,
            is_icmp_port_unreachable,
            is_tcp_reset,
        }),
    }
}

fn inspect_ipv6_packet(packet: &[u8], direction: Direction) -> PacketInspection {
    if packet.len() < IPV6_HEADER_LEN {
        return PacketInspection::unreadable();
    }

    let ip_packet = Ipv6Packet::new_unchecked(packet);
    let headers = walk_ipv6_headers(packet);
    if !headers.resolved {
        return PacketInspection {
            is_fragment: headers.is_fragment,
            metadata: Err(PacketMetadataError::UnresolvedIpv6Headers),
        };
    }

    let transport = packet.get(headers.transport_offset..).unwrap_or_default();
    let (source_port, destination_port) = get_ports(transport, headers.protocol);
    let key = build_key(
        direction,
        headers.protocol,
        IpAddress::Ipv6(ip_packet.src_addr()),
        source_port,
        IpAddress::Ipv6(ip_packet.dst_addr()),
        destination_port,
    );

    let icmp_echo = if headers.protocol == IpProtocol::Icmpv6 {
        get_icmp_echo(transport, true)
    } else {
        None
    };

    let outbound = matches!(direction, Direction::Outbound);
    let total_len = IPV6_HEADER_LEN + ip_packet.payload_len() as usize;
    let is_icmp_port_unreachable = headers.protocol == IpProtocol::Icmpv6
        && ip_packet.version() == 6
        && is_port_unreachable(packet, headers.transport_offset, total_len, true);
    let is_tcp_reset = outbound
        && headers.protocol == IpProtocol::Tcp
        && has_tcp_reset(packet, headers.transport_offset);

    PacketInspection {
        is_fragment: headers.is_fragment,
        metadata: Ok(PacketMetadata {
            key,
            icmp_echo,
            is_icmp_port_unreachable,
            is_tcp_reset,
        }),
    }
}

fn build_key(
    direction: Direction,
    protocol: IpProtocol,
    source_address: IpAddress,
    source_port: u16,
    destination_address: IpAddress,
    destination_port: u16,
) -> Key {
    match direction {
        Direction::Outbound => Key {
            protocol,
            local_address: source_address,
            local_port: source_port,
            remote_address: destination_address,
            remote_port: destination_port,
        },
        Direction::Inbound => Key {
            protocol,
            local_address: destination_address,
            local_port: destination_port,
            remote_address: source_address,
            remote_port: source_port,
        },
    }
}

fn get_ports(transport: &[u8], protocol: IpProtocol) -> (u16, u16) {
    if !matches!(protocol, IpProtocol::Tcp | IpProtocol::Udp) || transport.len() < 4 {
        return (0, 0);
    }

    (
        u16::from_be_bytes([transport[0], transport[1]]),
        u16::from_be_bytes([transport[2], transport[3]]),
    )
}

fn get_icmp_echo(transport: &[u8], ipv6: bool) -> Option<IcmpEcho> {
    if transport.len() < ICMP_ECHO_ID_END {
        return None;
    }

    let message_type = transport[0];
    let is_request = if ipv6 {
        message_type == 128
    } else {
        message_type == 8
    };
    let is_reply = if ipv6 {
        message_type == 129
    } else {
        message_type == 0
    };
    if !is_request && !is_reply {
        return None;
    }

    Some(IcmpEcho {
        is_request,
        identifier: u16::from_be_bytes([transport[4], transport[5]]),
    })
}

fn has_tcp_reset(packet: &[u8], transport_offset: usize) -> bool {
    transport_offset
        .checked_add(TCP_FLAGS_OFFSET)
        .and_then(|offset| packet.get(offset))
        .is_some_and(|flags| flags & TCP_RST_FLAG != 0)
}

fn is_port_unreachable(
    packet: &[u8],
    transport_offset: usize,
    total_len: usize,
    ipv6: bool,
) -> bool {
    let max_header_len = if ipv6 {
        MAX_PACKET_INSPECT_LEN
    } else {
        IPV4_MAX_HEADER_LEN
    };
    if !(if ipv6 {
        IPV6_HEADER_LEN
    } else {
        IPV4_HEADER_LEN
    }..=max_header_len)
        .contains(&transport_offset)
    {
        return false;
    }

    let Some(header_end) = transport_offset.checked_add(ICMP_HEADER_LEN) else {
        return false;
    };
    if total_len < header_end {
        return false;
    }
    let Some(header) = packet.get(transport_offset..header_end) else {
        return false;
    };

    let (message_type, code) = if ipv6 {
        (
            ICMPV6_TYPE_DESTINATION_UNREACHABLE,
            ICMPV6_CODE_DU_PORT_UNREACHABLE,
        )
    } else {
        (
            ICMPV4_TYPE_DESTINATION_UNREACHABLE,
            ICMPV4_CODE_DU_PORT_UNREACHABLE,
        )
    };
    header[0] == message_type && header[1] == code
}

#[cfg(test)]
mod tests {
    use super::*;
    use smoltcp::wire::{Ipv4Address, Ipv6Address};

    fn set_ipv4_endpoints(packet: &mut [u8]) {
        packet[12..16].copy_from_slice(&[192, 0, 2, 1]);
        packet[16..20].copy_from_slice(&[198, 51, 100, 2]);
    }

    #[test]
    fn inspects_ipv4_options_and_tcp_reset_once() {
        let mut packet = [0u8; 80];
        packet[0] = 0x4f;
        packet[2..4].copy_from_slice(&80u16.to_be_bytes());
        packet[9] = u8::from(IpProtocol::Tcp);
        set_ipv4_endpoints(&mut packet);
        packet[60..62].copy_from_slice(&50_000u16.to_be_bytes());
        packet[62..64].copy_from_slice(&443u16.to_be_bytes());
        packet[73] = TCP_RST_FLAG;

        let inspected = inspect_packet(&packet, false, Direction::Outbound);
        assert!(!inspected.is_fragment);
        let metadata = inspected.metadata.expect("IPv4 metadata");
        assert_eq!(metadata.key.protocol, IpProtocol::Tcp);
        assert_eq!(metadata.key.local_port, 50_000);
        assert_eq!(metadata.key.remote_port, 443);
        assert_eq!(
            metadata.key.local_address,
            IpAddress::Ipv4(Ipv4Address::new(192, 0, 2, 1))
        );
        assert!(metadata.is_tcp_reset);
    }

    #[test]
    fn identifies_fragment_even_when_too_short_for_key() {
        let mut packet = [0u8; IPV4_HEADER_LEN];
        packet[0] = 0x45;
        packet[6..8].copy_from_slice(&0x2000u16.to_be_bytes());

        let inspected = inspect_packet(&packet, false, Direction::Outbound);
        assert!(inspected.is_fragment);
        assert!(matches!(
            inspected.metadata,
            Err(PacketMetadataError::UnreadablePacket)
        ));
    }

    #[test]
    fn extracts_ipv4_echo_and_port_unreachable_metadata() {
        let mut packet = [0u8; IPV4_HEADER_LEN + ICMP_HEADER_LEN];
        packet[0] = 0x45;
        packet[2..4].copy_from_slice(&28u16.to_be_bytes());
        packet[9] = u8::from(IpProtocol::Icmp);
        set_ipv4_endpoints(&mut packet);
        packet[20] = 8;
        packet[24..26].copy_from_slice(&0x1234u16.to_be_bytes());

        let metadata = inspect_packet(&packet, false, Direction::Outbound)
            .metadata
            .expect("ICMP echo metadata");
        let echo = metadata.icmp_echo.expect("echo request");
        assert!(echo.is_request);
        assert_eq!(echo.identifier, 0x1234);
        assert!(!metadata.is_icmp_port_unreachable);

        packet[20] = ICMPV4_TYPE_DESTINATION_UNREACHABLE;
        packet[21] = ICMPV4_CODE_DU_PORT_UNREACHABLE;
        let metadata = inspect_packet(&packet, false, Direction::Outbound)
            .metadata
            .expect("ICMP unreachable metadata");
        assert!(metadata.icmp_echo.is_none());
        assert!(metadata.is_icmp_port_unreachable);

        let inbound = inspect_packet(&packet, false, Direction::Inbound)
            .metadata
            .expect("inbound ICMP unreachable metadata");
        assert!(inbound.is_icmp_port_unreachable);
    }

    #[test]
    fn recognizes_captured_ipv4_port_unreachable() {
        let packet = [
            0x45, 0x00, 0x00, 0x39, 0x55, 0x3a, 0x00, 0x00, 0x80, 0x01, 0xad, 0x97, 0xc0, 0xa8,
            0xdb, 0x10, 0xc0, 0xa8, 0xdb, 0x90, 0x03, 0x03, 0x35, 0x0a, 0x00, 0x00, 0x00, 0x00,
            0x45, 0x00, 0x00, 0x1d, 0x0e, 0x62, 0x00, 0x00, 0x80, 0x11, 0xf4, 0x7b, 0xc0, 0xa8,
            0xdb, 0x90, 0xc0, 0xa8, 0xdb, 0x10, 0xa9, 0x07, 0x04, 0xd2, 0x00, 0x09, 0x1a, 0x10,
            0x00,
        ];

        let metadata = inspect_packet(&packet, false, Direction::Outbound)
            .metadata
            .expect("captured ICMPv4 metadata");
        assert!(metadata.is_icmp_port_unreachable);
    }

    #[test]
    fn inspects_ipv6_tcp_after_extension_header() {
        const TRANSPORT_OFFSET: usize = IPV6_HEADER_LEN + 8;
        let mut packet = [0u8; TRANSPORT_OFFSET + 20];
        packet[0] = 0x60;
        packet[4..6].copy_from_slice(&28u16.to_be_bytes());
        packet[6] = u8::from(IpProtocol::Ipv6Opts);
        packet[8..24].copy_from_slice(&Ipv6Address::LOOPBACK.0);
        let remote = Ipv6Address::new(0x2001, 0xdb8, 0, 0, 0, 0, 0, 2);
        packet[24..40].copy_from_slice(&remote.0);
        packet[40] = u8::from(IpProtocol::Tcp);
        packet[41] = 0;
        packet[TRANSPORT_OFFSET..TRANSPORT_OFFSET + 2].copy_from_slice(&50_001u16.to_be_bytes());
        packet[TRANSPORT_OFFSET + 2..TRANSPORT_OFFSET + 4].copy_from_slice(&443u16.to_be_bytes());
        packet[TRANSPORT_OFFSET + TCP_FLAGS_OFFSET] = TCP_RST_FLAG;

        let metadata = inspect_packet(&packet, true, Direction::Outbound)
            .metadata
            .expect("IPv6 metadata");
        assert_eq!(metadata.key.local_port, 50_001);
        assert_eq!(metadata.key.remote_port, 443);
        assert_eq!(metadata.key.remote_address, IpAddress::Ipv6(remote));
        assert!(metadata.is_tcp_reset);
    }

    #[test]
    fn reports_ipv6_fragment_independently_from_transport_key() {
        let mut packet = [0u8; IPV6_HEADER_LEN + 8];
        packet[0] = 0x60;
        packet[4..6].copy_from_slice(&8u16.to_be_bytes());
        packet[6] = u8::from(IpProtocol::Ipv6Frag);
        packet[40] = u8::from(IpProtocol::Udp);
        packet[43] = 1;

        let inspected = inspect_packet(&packet, true, Direction::Inbound);
        assert!(inspected.is_fragment);
        let metadata = inspected.metadata.expect("fragment metadata");
        assert_eq!(metadata.key.protocol, IpProtocol::Udp);
        assert_eq!(metadata.key.local_port, 0);
        assert_eq!(metadata.key.remote_port, 0);
    }

    #[test]
    fn recognizes_ipv6_port_unreachable_after_extension_header() {
        let mut packet = [0u8; IPV6_HEADER_LEN + 16];
        packet[0] = 0x60;
        packet[4..6].copy_from_slice(&16u16.to_be_bytes());
        packet[6] = u8::from(IpProtocol::Ipv6Opts);
        packet[40] = u8::from(IpProtocol::Icmpv6);
        packet[41] = 0;
        packet[48] = ICMPV6_TYPE_DESTINATION_UNREACHABLE;
        packet[49] = ICMPV6_CODE_DU_PORT_UNREACHABLE;

        let outbound = inspect_packet(&packet, true, Direction::Outbound)
            .metadata
            .expect("outbound ICMPv6 metadata");
        assert!(outbound.is_icmp_port_unreachable);

        let inbound = inspect_packet(&packet, true, Direction::Inbound)
            .metadata
            .expect("inbound ICMPv6 metadata");
        assert!(inbound.is_icmp_port_unreachable);
    }

    #[test]
    fn rejects_unresolved_ipv6_extension_chain() {
        let mut packet = [0u8; IPV6_HEADER_LEN + 9 * 8];
        packet[0] = 0x60;
        packet[4..6].copy_from_slice(&(9u16 * 8).to_be_bytes());
        packet[6] = u8::from(IpProtocol::Ipv6Opts);
        for index in 0..9 {
            let offset = IPV6_HEADER_LEN + index * 8;
            packet[offset] = if index == 8 {
                u8::from(IpProtocol::Udp)
            } else {
                u8::from(IpProtocol::Ipv6Opts)
            };
        }

        let inspected = inspect_packet(&packet, true, Direction::Outbound);
        assert!(matches!(
            inspected.metadata,
            Err(PacketMetadataError::UnresolvedIpv6Headers)
        ));
    }
}
