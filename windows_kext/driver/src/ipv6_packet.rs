use core::fmt;

use smoltcp::wire::{
    IpAddress, IpProtocol, Ipv6Address, Ipv6ExtHeader, Ipv6FragmentHeader, Ipv6Packet, TcpPacket,
    UdpPacket, IPV6_HEADER_LEN,
};

/// Maximum number of IPv6 extension headers followed before a packet is
/// considered unresolved. This bounds work performed by a packet callout at
/// DISPATCH_LEVEL.
const MAX_IPV6_EXT_HEADERS: usize = 8;

/// Result of walking an IPv6 extension-header chain.
pub(crate) struct Ipv6Headers {
    /// The upper-layer protocol after all supported extension headers.
    pub(crate) protocol: IpProtocol,
    /// Byte offset of the upper-layer header from the IPv6 base header.
    pub(crate) transport_offset: usize,
    /// True for a non-atomic fragment. Its transport checksum cannot be
    /// recalculated from this packet alone.
    pub(crate) is_fragment: bool,
    /// A routing header with Segments Left greater than zero was encountered.
    /// In that case the final destination needed by the pseudo-header is not
    /// necessarily the destination in the IPv6 base header.
    active_routing_header: bool,
    /// True when the walk reached an upper-layer protocol within the limit.
    pub(crate) resolved: bool,
}

/// Walks the supported IPv6 extension headers and returns the upper-layer
/// protocol and offset. `packet` must begin at the IPv6 base header.
///
/// The walker understands Hop-by-Hop Options, Routing, Fragment and Destination
/// Options headers. An exact chain of `MAX_IPV6_EXT_HEADERS` is accepted when
/// the eighth header points to an upper-layer protocol; a ninth extension
/// header remains unresolved.
pub(crate) fn walk_ipv6_headers(packet: &[u8]) -> Ipv6Headers {
    let mut result = Ipv6Headers {
        protocol: IpProtocol::Unknown(0),
        transport_offset: IPV6_HEADER_LEN,
        is_fragment: false,
        active_routing_header: false,
        resolved: false,
    };

    if packet.len() < IPV6_HEADER_LEN {
        return result;
    }

    let mut protocol = Ipv6Packet::new_unchecked(packet).next_header();
    let mut offset = IPV6_HEADER_LEN;
    let mut extension_count = 0;

    loop {
        match protocol {
            IpProtocol::Ipv6Frag => {
                if extension_count >= MAX_IPV6_EXT_HEADERS {
                    break;
                }
                extension_count += 1;

                // The complete Fragment header is eight bytes. smoltcp's
                // Ipv6FragmentHeader models only the six bytes after Next Header
                // and Reserved, so pass exactly that sub-slice to it.
                let Some(end) = offset.checked_add(8) else {
                    break;
                };
                let Some(header) = packet.get(offset..end) else {
                    break;
                };
                let Ok(fragment) = Ipv6FragmentHeader::new_checked(&header[2..]) else {
                    break;
                };

                if fragment.frag_offset() != 0 || fragment.more_frags() {
                    result.is_fragment = true;
                }

                protocol = IpProtocol::from(header[0]);
                offset += 8;
            }
            IpProtocol::HopByHop | IpProtocol::Ipv6Route | IpProtocol::Ipv6Opts => {
                if extension_count >= MAX_IPV6_EXT_HEADERS {
                    break;
                }
                extension_count += 1;

                let Some(rest) = packet.get(offset..) else {
                    break;
                };
                let Ok(extension) = Ipv6ExtHeader::new_checked(rest) else {
                    break;
                };

                // A checksum pseudo-header uses the final destination when a
                // Routing header is active. Generic routing types encode that
                // address differently, so checksum rewriting is rejected below
                // unless Segments Left is zero and the base destination is final.
                if protocol == IpProtocol::Ipv6Route
                    && rest.get(3).copied().unwrap_or_default() != 0
                {
                    result.active_routing_header = true;
                }

                // Hdr Ext Len counts eight-octet units after the first eight.
                let length = (extension.header_len() as usize + 1) * 8;
                protocol = extension.next_header();
                let Some(next_offset) = offset.checked_add(length) else {
                    break;
                };
                offset = next_offset;
            }
            _ => {
                // Reaching this branch after parsing the eighth extension header
                // is valid. The old fixed-range loop stopped one iteration too
                // early and incorrectly left such a chain unresolved.
                result.resolved = true;
                break;
            }
        }
    }

    result.protocol = protocol;
    result.transport_offset = offset;
    result
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum Ipv6TransportError {
    InvalidPacket,
    UnresolvedHeaders,
    FragmentedPacket,
    ActiveRoutingHeader,
    UnsupportedProtocol,
    InvalidTransportHeader,
}

impl fmt::Display for Ipv6TransportError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        let message = match self {
            Self::InvalidPacket => "invalid IPv6 packet",
            Self::UnresolvedHeaders => "IPv6 extension header chain did not resolve",
            Self::FragmentedPacket => "cannot rewrite an individual IPv6 fragment",
            Self::ActiveRoutingHeader => {
                "cannot calculate a checksum through an active IPv6 routing header"
            }
            Self::UnsupportedProtocol => "IPv6 packet does not contain TCP or UDP",
            Self::InvalidTransportHeader => "invalid IPv6 transport header",
        };
        formatter.write_str(message)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum PortRewrite {
    Source(u16),
    Destination(u16),
}

struct TransportContext {
    protocol: IpProtocol,
    offset: usize,
    total_len: usize,
    source: Ipv6Address,
    destination: Ipv6Address,
}

fn transport_context(packet: &[u8]) -> Result<TransportContext, Ipv6TransportError> {
    let ip_packet =
        Ipv6Packet::new_checked(packet).map_err(|_| Ipv6TransportError::InvalidPacket)?;
    if ip_packet.version() != 6 {
        return Err(Ipv6TransportError::InvalidPacket);
    }

    let total_len = ip_packet.total_len();
    // A zero Payload Length can denote a jumbogram, whose 32-bit length lives
    // in the Hop-by-Hop Jumbo Payload option. smoltcp exposes only the base
    // 16-bit field here, so treating the trailing bytes as a normal transport
    // segment would calculate the wrong pseudo-header length.
    if ip_packet.payload_len() == 0 && packet.len() > IPV6_HEADER_LEN {
        return Err(Ipv6TransportError::InvalidPacket);
    }
    let headers = walk_ipv6_headers(&packet[..total_len]);
    if !headers.resolved || headers.transport_offset > total_len {
        return Err(Ipv6TransportError::UnresolvedHeaders);
    }
    if headers.is_fragment {
        return Err(Ipv6TransportError::FragmentedPacket);
    }
    if headers.active_routing_header {
        return Err(Ipv6TransportError::ActiveRoutingHeader);
    }

    Ok(TransportContext {
        protocol: headers.protocol,
        offset: headers.transport_offset,
        total_len,
        source: ip_packet.src_addr(),
        destination: ip_packet.dst_addr(),
    })
}

fn validate_transport(packet: &[u8], context: &TransportContext) -> Result<(), Ipv6TransportError> {
    let transport = packet
        .get(context.offset..context.total_len)
        .ok_or(Ipv6TransportError::InvalidTransportHeader)?;

    match context.protocol {
        IpProtocol::Udp => UdpPacket::new_checked(transport)
            .map(|_| ())
            .map_err(|_| Ipv6TransportError::InvalidTransportHeader),
        IpProtocol::Tcp => TcpPacket::new_checked(transport)
            .map(|_| ())
            .map_err(|_| Ipv6TransportError::InvalidTransportHeader),
        _ => Err(Ipv6TransportError::UnsupportedProtocol),
    }
}

/// Rewrites both IPv6 addresses and one TCP/UDP port, then computes the
/// upper-layer checksum over the bytes after the extension-header chain.
///
/// Validation is completed before any byte is modified, so malformed,
/// fragmented or unsupported packets are returned unchanged.
pub(crate) fn rewrite_ipv6_tcp_udp(
    packet: &mut [u8],
    source: Ipv6Address,
    destination: Ipv6Address,
    port: PortRewrite,
) -> Result<(), Ipv6TransportError> {
    let context = transport_context(packet)?;
    validate_transport(packet, &context)?;

    // The base header was fully validated above and neither operation changes a
    // length field, so the transport offset remains valid after this mutation.
    let mut ip_packet = Ipv6Packet::new_unchecked(&mut packet[..context.total_len]);
    ip_packet.set_src_addr(source);
    ip_packet.set_dst_addr(destination);

    let transport = packet
        .get_mut(context.offset..context.total_len)
        .ok_or(Ipv6TransportError::InvalidTransportHeader)?;
    let source = IpAddress::Ipv6(source);
    let destination = IpAddress::Ipv6(destination);

    match context.protocol {
        IpProtocol::Udp => {
            let mut udp_packet = UdpPacket::new_checked(transport)
                .map_err(|_| Ipv6TransportError::InvalidTransportHeader)?;
            match port {
                PortRewrite::Source(value) => udp_packet.set_src_port(value),
                PortRewrite::Destination(value) => udp_packet.set_dst_port(value),
            }
            udp_packet.fill_checksum(&source, &destination);
        }
        IpProtocol::Tcp => {
            let mut tcp_packet = TcpPacket::new_checked(transport)
                .map_err(|_| Ipv6TransportError::InvalidTransportHeader)?;
            match port {
                PortRewrite::Source(value) => tcp_packet.set_src_port(value),
                PortRewrite::Destination(value) => tcp_packet.set_dst_port(value),
            }
            tcp_packet.fill_checksum(&source, &destination);
        }
        _ => return Err(Ipv6TransportError::UnsupportedProtocol),
    }

    Ok(())
}

/// Recomputes a TCP or UDP checksum using the upper-layer length, excluding all
/// IPv6 extension headers from the pseudo-header length.
pub(crate) fn recalculate_ipv6_transport_checksum(
    packet: &mut [u8],
) -> Result<(), Ipv6TransportError> {
    let context = transport_context(packet)?;

    // Preserve the old behavior for non-TCP/UDP traffic: there is no transport
    // checksum for this routine to update.
    if !matches!(context.protocol, IpProtocol::Tcp | IpProtocol::Udp) {
        return Ok(());
    }
    validate_transport(packet, &context)?;

    let transport = packet
        .get_mut(context.offset..context.total_len)
        .ok_or(Ipv6TransportError::InvalidTransportHeader)?;
    let source = IpAddress::Ipv6(context.source);
    let destination = IpAddress::Ipv6(context.destination);

    match context.protocol {
        IpProtocol::Udp => UdpPacket::new_checked(transport)
            .map_err(|_| Ipv6TransportError::InvalidTransportHeader)?
            .fill_checksum(&source, &destination),
        IpProtocol::Tcp => TcpPacket::new_checked(transport)
            .map_err(|_| Ipv6TransportError::InvalidTransportHeader)?
            .fill_checksum(&source, &destination),
        _ => {}
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn source_address() -> Ipv6Address {
        Ipv6Address::new(0x2001, 0xdb8, 1, 2, 3, 4, 5, 6)
    }

    fn destination_address() -> Ipv6Address {
        Ipv6Address::new(0x2001, 0xdb8, 7, 8, 9, 10, 11, 12)
    }

    fn initialize_ipv6(
        packet: &mut [u8],
        next_header: IpProtocol,
        source: Ipv6Address,
        destination: Ipv6Address,
    ) {
        let payload_len = packet.len() - IPV6_HEADER_LEN;
        let mut ip_packet = Ipv6Packet::new_unchecked(packet);
        ip_packet.set_version(6);
        ip_packet.set_payload_len(payload_len as u16);
        ip_packet.set_next_header(next_header);
        ip_packet.set_hop_limit(64);
        ip_packet.set_src_addr(source);
        ip_packet.set_dst_addr(destination);
    }

    fn initialize_udp(
        packet: &mut [u8],
        offset: usize,
        source: Ipv6Address,
        destination: Ipv6Address,
        source_port: u16,
        destination_port: u16,
    ) {
        let length = packet.len() - offset;
        let mut udp_packet = UdpPacket::new_unchecked(&mut packet[offset..]);
        udp_packet.set_src_port(source_port);
        udp_packet.set_dst_port(destination_port);
        udp_packet.set_len(length as u16);
        udp_packet.fill_checksum(&IpAddress::Ipv6(source), &IpAddress::Ipv6(destination));
    }

    fn initialize_tcp(
        packet: &mut [u8],
        offset: usize,
        source: Ipv6Address,
        destination: Ipv6Address,
        source_port: u16,
        destination_port: u16,
    ) {
        let mut tcp_packet = TcpPacket::new_unchecked(&mut packet[offset..]);
        tcp_packet.set_src_port(source_port);
        tcp_packet.set_dst_port(destination_port);
        tcp_packet.set_header_len(20);
        tcp_packet.fill_checksum(&IpAddress::Ipv6(source), &IpAddress::Ipv6(destination));
    }

    fn options_chain(extension_count: usize) -> Vec<u8> {
        let transport_offset = IPV6_HEADER_LEN + extension_count * 8;
        let mut packet = vec![0u8; transport_offset + 8];
        initialize_ipv6(
            &mut packet,
            IpProtocol::Ipv6Opts,
            source_address(),
            destination_address(),
        );

        for index in 0..extension_count {
            let offset = IPV6_HEADER_LEN + index * 8;
            packet[offset] = if index + 1 == extension_count {
                u8::from(IpProtocol::Udp)
            } else {
                u8::from(IpProtocol::Ipv6Opts)
            };
            packet[offset + 1] = 0;
        }

        initialize_udp(
            &mut packet,
            transport_offset,
            source_address(),
            destination_address(),
            1000,
            2000,
        );
        packet
    }

    #[test]
    fn resolves_exactly_eight_extension_headers() {
        let packet = options_chain(MAX_IPV6_EXT_HEADERS);
        let headers = walk_ipv6_headers(&packet);

        assert!(headers.resolved);
        assert_eq!(headers.protocol, IpProtocol::Udp);
        assert_eq!(
            headers.transport_offset,
            IPV6_HEADER_LEN + MAX_IPV6_EXT_HEADERS * 8
        );
    }

    #[test]
    fn rejects_a_ninth_extension_header() {
        let packet = options_chain(MAX_IPV6_EXT_HEADERS + 1);
        let headers = walk_ipv6_headers(&packet);

        assert!(!headers.resolved);
        assert_eq!(headers.protocol, IpProtocol::Ipv6Opts);
    }

    #[test]
    fn reads_fragment_fields_after_next_header_and_reserved() {
        let transport_offset = IPV6_HEADER_LEN + 8;
        let mut packet = vec![0u8; transport_offset + 8];
        initialize_ipv6(
            &mut packet,
            IpProtocol::Ipv6Frag,
            source_address(),
            destination_address(),
        );
        packet[IPV6_HEADER_LEN] = u8::from(IpProtocol::Udp);

        let atomic = walk_ipv6_headers(&packet);
        assert!(atomic.resolved);
        assert!(!atomic.is_fragment);
        assert_eq!(atomic.protocol, IpProtocol::Udp);
        assert_eq!(atomic.transport_offset, transport_offset);

        // The M bit is in byte three of the complete Fragment header, not in
        // the Next Header/Reserved pair at bytes zero and one.
        packet[IPV6_HEADER_LEN + 3] = 1;
        assert!(walk_ipv6_headers(&packet).is_fragment);
    }

    #[test]
    fn rewrites_udp_after_multiple_extension_headers() {
        let source = source_address();
        let destination = destination_address();
        let transport_offset = IPV6_HEADER_LEN + 16;
        let mut packet = vec![0u8; transport_offset + 12];
        initialize_ipv6(&mut packet, IpProtocol::HopByHop, source, destination);
        packet[IPV6_HEADER_LEN] = u8::from(IpProtocol::Ipv6Opts);
        packet[IPV6_HEADER_LEN + 1] = 0;
        packet[IPV6_HEADER_LEN + 8] = u8::from(IpProtocol::Udp);
        packet[IPV6_HEADER_LEN + 9] = 0;
        initialize_udp(
            &mut packet,
            transport_offset,
            source,
            destination,
            12000,
            53,
        );
        let extension_headers = packet[IPV6_HEADER_LEN..transport_offset].to_vec();

        let redirected_source = Ipv6Address::LOOPBACK;
        let redirected_destination = Ipv6Address::new(0xfd00, 1, 2, 3, 4, 5, 6, 7);
        rewrite_ipv6_tcp_udp(
            &mut packet,
            redirected_source,
            redirected_destination,
            PortRewrite::Destination(5353),
        )
        .unwrap();

        let ip_packet = Ipv6Packet::new_checked(&packet).unwrap();
        assert_eq!(ip_packet.src_addr(), redirected_source);
        assert_eq!(ip_packet.dst_addr(), redirected_destination);
        assert_eq!(
            &packet[IPV6_HEADER_LEN..transport_offset],
            &extension_headers
        );

        let udp_packet = UdpPacket::new_checked(&packet[transport_offset..]).unwrap();
        assert_eq!(udp_packet.src_port(), 12000);
        assert_eq!(udp_packet.dst_port(), 5353);
        assert!(udp_packet.verify_checksum(
            &IpAddress::Ipv6(redirected_source),
            &IpAddress::Ipv6(redirected_destination),
        ));
    }

    #[test]
    fn rewrites_tcp_after_inactive_routing_header() {
        let source = source_address();
        let destination = destination_address();
        let transport_offset = IPV6_HEADER_LEN + 8;
        let mut packet = vec![0u8; transport_offset + 24];
        initialize_ipv6(&mut packet, IpProtocol::Ipv6Route, source, destination);
        packet[IPV6_HEADER_LEN] = u8::from(IpProtocol::Tcp);
        packet[IPV6_HEADER_LEN + 1] = 0;
        packet[IPV6_HEADER_LEN + 2] = 4;
        packet[IPV6_HEADER_LEN + 3] = 0;
        initialize_tcp(
            &mut packet,
            transport_offset,
            source,
            destination,
            443,
            40000,
        );

        let redirected_source = Ipv6Address::new(0x2001, 0xdb8, 20, 0, 0, 0, 0, 1);
        let redirected_destination = Ipv6Address::new(0x2001, 0xdb8, 30, 0, 0, 0, 0, 2);
        rewrite_ipv6_tcp_udp(
            &mut packet,
            redirected_source,
            redirected_destination,
            PortRewrite::Source(8443),
        )
        .unwrap();

        let tcp_packet = TcpPacket::new_checked(&packet[transport_offset..]).unwrap();
        assert_eq!(tcp_packet.src_port(), 8443);
        assert_eq!(tcp_packet.dst_port(), 40000);
        assert!(tcp_packet.verify_checksum(
            &IpAddress::Ipv6(redirected_source),
            &IpAddress::Ipv6(redirected_destination),
        ));
    }

    #[test]
    fn recalculates_checksum_from_transport_offset() {
        let mut packet = options_chain(2);
        let headers = walk_ipv6_headers(&packet);
        let checksum_offset = headers.transport_offset + 6;
        packet[checksum_offset] = 0;
        packet[checksum_offset + 1] = 0;

        recalculate_ipv6_transport_checksum(&mut packet).unwrap();

        let udp_packet = UdpPacket::new_checked(&packet[headers.transport_offset..]).unwrap();
        assert!(udp_packet.verify_checksum(
            &IpAddress::Ipv6(source_address()),
            &IpAddress::Ipv6(destination_address()),
        ));
    }

    #[test]
    fn leaves_unresolved_packet_unchanged() {
        let mut packet = options_chain(1);
        packet[IPV6_HEADER_LEN + 1] = 10;
        let original = packet.clone();

        assert_eq!(
            rewrite_ipv6_tcp_udp(
                &mut packet,
                Ipv6Address::LOOPBACK,
                destination_address(),
                PortRewrite::Destination(5353),
            ),
            Err(Ipv6TransportError::UnresolvedHeaders)
        );
        assert_eq!(packet, original);
    }

    #[test]
    fn rejects_active_routing_header_without_mutation() {
        let source = source_address();
        let destination = destination_address();
        let transport_offset = IPV6_HEADER_LEN + 8;
        let mut packet = vec![0u8; transport_offset + 20];
        initialize_ipv6(&mut packet, IpProtocol::Ipv6Route, source, destination);
        packet[IPV6_HEADER_LEN] = u8::from(IpProtocol::Tcp);
        packet[IPV6_HEADER_LEN + 1] = 0;
        packet[IPV6_HEADER_LEN + 2] = 4;
        packet[IPV6_HEADER_LEN + 3] = 1;
        initialize_tcp(
            &mut packet,
            transport_offset,
            source,
            destination,
            443,
            40000,
        );
        let original = packet.clone();

        assert_eq!(
            rewrite_ipv6_tcp_udp(
                &mut packet,
                Ipv6Address::LOOPBACK,
                destination,
                PortRewrite::Source(8443),
            ),
            Err(Ipv6TransportError::ActiveRoutingHeader)
        );
        assert_eq!(packet, original);
    }
}
