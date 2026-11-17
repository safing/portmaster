//! Layer 3/4 header parsing, port of Go `network/packet/parse.go`.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};

use pm_netstate::{IpProtocol, IpVersion};

pub const PROTO_ICMP: u8 = 1;
pub const PROTO_IGMP: u8 = 2;
pub const PROTO_TCP: u8 = 6;
pub const PROTO_UDP: u8 = 17;
pub const PROTO_ICMPV6: u8 = 58;

/// Why a packet could not be parsed; such packets are dropped (like Go).
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum ParseError {
    #[error("empty packet")]
    Empty,
    #[error("unknown IP version or network protocol: {0:02x}")]
    UnknownVersion(u8),
    #[error("truncated {0} header")]
    Truncated(&'static str),
}

/// Header data of an intercepted packet.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Parsed {
    pub version: IpVersion,
    pub src: IpAddr,
    pub dst: IpAddr,
    /// IP protocol number of the transport layer.
    pub protocol: u8,
    pub src_port: u16,
    pub dst_port: u16,
    /// Offset of the transport payload (layer 5 data) in the packet, if any.
    pub payload_offset: Option<usize>,
}

impl Parsed {
    /// Protocol in the form used by the netstate lookups.
    pub fn ip_protocol(&self) -> IpProtocol {
        match self.protocol {
            PROTO_TCP => IpProtocol::TCP,
            PROTO_UDP => IpProtocol::UDP,
            PROTO_ICMP | PROTO_ICMPV6 => IpProtocol::Icmp,
            _ => IpProtocol::Unknown,
        }
    }
}

/// Parses an IP packet as delivered by nfqueue.
pub fn parse_layer3(data: &[u8]) -> Result<Parsed, ParseError> {
    let first = *data.first().ok_or(ParseError::Empty)?;
    match first >> 4 {
        4 => parse_ipv4(data),
        6 => parse_ipv6(data),
        v => Err(ParseError::UnknownVersion(v)),
    }
}

fn parse_ipv4(data: &[u8]) -> Result<Parsed, ParseError> {
    if data.len() < 20 {
        return Err(ParseError::Truncated("IPv4"));
    }
    let ihl = usize::from(data[0] & 0x0f) * 4;
    if ihl < 20 || data.len() < ihl {
        return Err(ParseError::Truncated("IPv4"));
    }
    let fragment_offset = u16::from_be_bytes([data[6], data[7]]) & 0x1fff;
    let src = Ipv4Addr::new(data[12], data[13], data[14], data[15]);
    let dst = Ipv4Addr::new(data[16], data[17], data[18], data[19]);
    let mut parsed = Parsed {
        version: IpVersion::V4,
        src: IpAddr::V4(src),
        dst: IpAddr::V4(dst),
        protocol: data[9],
        src_port: 0,
        dst_port: 0,
        payload_offset: None,
    };
    // Non-first fragments carry no transport header.
    if fragment_offset == 0 {
        parse_transport(&mut parsed, data, ihl)?;
    }
    Ok(parsed)
}

fn parse_ipv6(data: &[u8]) -> Result<Parsed, ParseError> {
    if data.len() < 40 {
        return Err(ParseError::Truncated("IPv6"));
    }
    let src: [u8; 16] = data[8..24]
        .try_into()
        .map_err(|_| ParseError::Truncated("IPv6"))?;
    let dst: [u8; 16] = data[24..40]
        .try_into()
        .map_err(|_| ParseError::Truncated("IPv6"))?;
    let (next, offset, first_fragment) = walk_ipv6_headers(data)?;

    let mut parsed = Parsed {
        version: IpVersion::V6,
        src: IpAddr::V6(Ipv6Addr::from(src)),
        dst: IpAddr::V6(Ipv6Addr::from(dst)),
        protocol: next,
        src_port: 0,
        dst_port: 0,
        payload_offset: None,
    };
    if first_fragment {
        parse_transport(&mut parsed, data, offset)?;
    }
    Ok(parsed)
}

/// Walks the IPv6 extension headers; returns the transport protocol, the
/// offset of the transport header and whether this is the first fragment.
fn walk_ipv6_headers(data: &[u8]) -> Result<(u8, usize, bool), ParseError> {
    let mut next = data[6];
    let mut offset = 40usize;
    let mut first_fragment = true;

    loop {
        let header_len = match next {
            0 | 43 | 60 => {
                let len = *data
                    .get(offset + 1)
                    .ok_or(ParseError::Truncated("IPv6 extension"))?;
                (usize::from(len) + 1) * 8
            }
            44 => {
                let frag = data
                    .get(offset + 2..offset + 4)
                    .ok_or(ParseError::Truncated("IPv6 fragment"))?;
                first_fragment = u16::from_be_bytes([frag[0], frag[1]]) >> 3 == 0;
                8
            }
            51 => {
                let len = *data
                    .get(offset + 1)
                    .ok_or(ParseError::Truncated("IPv6 extension"))?;
                (usize::from(len) + 2) * 4
            }
            _ => break,
        };
        let header = data
            .get(offset..offset + header_len)
            .ok_or(ParseError::Truncated("IPv6 extension"))?;
        next = header[0];
        offset += header_len;
        if !first_fragment {
            break;
        }
    }
    Ok((next, offset, first_fragment))
}

/// Returns the ICMP/ICMPv6 type of an IP packet.
pub fn icmp_type(data: &[u8]) -> Option<u8> {
    let offset = match data.first()? >> 4 {
        4 => {
            let ihl = usize::from(data.first()? & 0x0f) * 4;
            if ihl < 20 || *data.get(9)? != PROTO_ICMP {
                return None;
            }
            ihl
        }
        6 => {
            if data.len() < 40 {
                return None;
            }
            let (next, offset, first_fragment) = walk_ipv6_headers(data).ok()?;
            if next != PROTO_ICMPV6 || !first_fragment {
                return None;
            }
            offset
        }
        _ => return None,
    };
    data.get(offset).copied()
}

fn parse_transport(parsed: &mut Parsed, data: &[u8], offset: usize) -> Result<(), ParseError> {
    let rest = &data[offset.min(data.len())..];
    match parsed.protocol {
        PROTO_TCP => {
            if rest.len() < 20 {
                return Err(ParseError::Truncated("TCP"));
            }
            let data_offset = usize::from(rest[12] >> 4) * 4;
            if data_offset < 20 || rest.len() < data_offset {
                return Err(ParseError::Truncated("TCP"));
            }
            parsed.src_port = u16::from_be_bytes([rest[0], rest[1]]);
            parsed.dst_port = u16::from_be_bytes([rest[2], rest[3]]);
            parsed.payload_offset = Some(offset + data_offset);
        }
        PROTO_UDP => {
            if rest.len() < 8 {
                return Err(ParseError::Truncated("UDP"));
            }
            parsed.src_port = u16::from_be_bytes([rest[0], rest[1]]);
            parsed.dst_port = u16::from_be_bytes([rest[2], rest[3]]);
            parsed.payload_offset = Some(offset + 8);
        }
        PROTO_ICMP | PROTO_ICMPV6 => {
            if rest.len() < 4 {
                return Err(ParseError::Truncated("ICMP"));
            }
        }
        _ => {}
    }
    Ok(())
}

/// Computes the RFC 1071 internet checksum over `data`.
/// Uses a 64-bit unrolled accumulator over 8-byte blocks followed by carry folding,
/// maximizing hardware instruction pipelining and auto-vectorization across x86_64 AVX2 / ARM NEON.
pub fn internet_checksum(data: &[u8]) -> u16 {
    let mut sum: u64 = 0;
    let mut chunks = data.chunks_exact(8);
    for chunk in &mut chunks {
        let w0 = u16::from_be_bytes([chunk[0], chunk[1]]) as u64;
        let w1 = u16::from_be_bytes([chunk[2], chunk[3]]) as u64;
        let w2 = u16::from_be_bytes([chunk[4], chunk[5]]) as u64;
        let w3 = u16::from_be_bytes([chunk[6], chunk[7]]) as u64;
        sum += w0 + w1 + w2 + w3;
    }
    let remainder = chunks.remainder();
    let mut chunks2 = remainder.chunks_exact(2);
    for chunk in &mut chunks2 {
        sum += u16::from_be_bytes([chunk[0], chunk[1]]) as u64;
    }
    let remainder = chunks2.remainder();
    if let Some(&b) = remainder.first() {
        sum += (u16::from(b) << 8) as u64;
    }

    while (sum >> 16) != 0 {
        sum = (sum & 0xffff) + (sum >> 16);
    }
    !(sum as u16)
}

/// Verifies whether an RFC 1071 internet checksum over `data` is valid (folds to 0).
#[inline]
pub fn verify_internet_checksum(data: &[u8]) -> bool {
    internet_checksum(data) == 0
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ipv4(protocol: u8, transport: &[u8]) -> Vec<u8> {
        let mut p = vec![
            0x45, 0, 0, 0, 0, 0, 0, 0, 64, protocol, 0, 0, 10, 0, 0, 1, 1, 1, 1, 1,
        ];
        let total = (20 + transport.len()) as u16;
        p[2..4].copy_from_slice(&total.to_be_bytes());
        p.extend_from_slice(transport);
        p
    }

    fn udp(src: u16, dst: u16) -> Vec<u8> {
        let mut u = Vec::new();
        u.extend_from_slice(&src.to_be_bytes());
        u.extend_from_slice(&dst.to_be_bytes());
        u.extend_from_slice(&[0, 8, 0, 0]);
        u
    }

    #[test]
    fn parses_ipv4_udp_and_tcp() {
        let p = parse_layer3(&ipv4(PROTO_UDP, &udp(5353, 53))).unwrap();
        assert_eq!(p.src, "10.0.0.1".parse::<IpAddr>().unwrap());
        assert_eq!(p.dst, "1.1.1.1".parse::<IpAddr>().unwrap());
        assert_eq!((p.src_port, p.dst_port), (5353, 53));
        assert_eq!(p.ip_protocol(), IpProtocol::UDP);
        assert_eq!(p.payload_offset, Some(28));

        let mut tcp = vec![0u8; 20];
        tcp[0..2].copy_from_slice(&40000u16.to_be_bytes());
        tcp[2..4].copy_from_slice(&443u16.to_be_bytes());
        tcp[12] = 5 << 4;
        let p = parse_layer3(&ipv4(PROTO_TCP, &tcp)).unwrap();
        assert_eq!(
            (p.src_port, p.dst_port, p.protocol),
            (40000, 443, PROTO_TCP)
        );
    }

    #[test]
    fn parses_ipv6_with_extension_header() {
        let mut p = vec![0x60, 0, 0, 0, 0, 0, 0, 64];
        p.extend_from_slice(&"2001:db8::1".parse::<Ipv6Addr>().unwrap().octets());
        p.extend_from_slice(&"2001:db8::2".parse::<Ipv6Addr>().unwrap().octets());
        // Hop-by-hop header (8 bytes) pointing to UDP.
        p[6] = 0;
        p.extend_from_slice(&[PROTO_UDP, 0, 0, 0, 0, 0, 0, 0]);
        p.extend_from_slice(&udp(1000, 53));
        let parsed = parse_layer3(&p).unwrap();
        assert_eq!(parsed.version, IpVersion::V6);
        assert_eq!(parsed.protocol, PROTO_UDP);
        assert_eq!(parsed.dst_port, 53);
    }

    #[test]
    fn reads_icmp_type() {
        let echo = ipv4(PROTO_ICMP, &[8, 0, 0, 0]);
        assert_eq!(icmp_type(&echo), Some(8));
        assert_eq!(icmp_type(&ipv4(PROTO_UDP, &udp(1, 2))), None);

        let mut p = vec![0x60, 0, 0, 0, 0, 0, PROTO_ICMPV6, 64];
        p.extend_from_slice(&"2001:db8::1".parse::<Ipv6Addr>().unwrap().octets());
        p.extend_from_slice(&"2001:db8::2".parse::<Ipv6Addr>().unwrap().octets());
        p.extend_from_slice(&[128, 0, 0, 0]);
        assert_eq!(icmp_type(&p), Some(128));
        assert_eq!(icmp_type(&[]), None);
        // Short or malformed input must not panic.
        for len in 0..echo.len() {
            let _ = icmp_type(&echo[..len]);
        }
        assert_eq!(icmp_type(&[0x45, 0, 0]), None);
        assert_eq!(icmp_type(&[0x41; 12]), None);
        assert_eq!(icmp_type(&[0x60; 10]), None);
        assert_eq!(icmp_type(&p[..40]), None);
    }

    #[test]
    fn truncated_packets_are_errors() {
        assert_eq!(parse_layer3(&[]), Err(ParseError::Empty));
        assert_eq!(parse_layer3(&[0x20]), Err(ParseError::UnknownVersion(2)));
        let full = ipv4(PROTO_UDP, &udp(1, 2));
        for len in 1..full.len() {
            assert!(parse_layer3(&full[..len]).is_err(), "len {len}");
        }
        // Non-first fragment: no transport header needed.
        let mut frag = ipv4(PROTO_UDP, &[]);
        frag[6] = 0x00;
        frag[7] = 0x10;
        assert_eq!(parse_layer3(&frag).unwrap().dst_port, 0);
    }

    #[test]
    fn rfc1071_internet_checksum_test() {
        // Standard RFC 1071 test vector
        let data = [0x00, 0x01, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7];
        let csum = internet_checksum(&data);
        assert_eq!(csum, 0x220d);

        // A packet with its checksum inserted folds to 0
        let mut pkt = data.to_vec();
        pkt.extend_from_slice(&csum.to_be_bytes());
        assert!(verify_internet_checksum(&pkt));

        // Odd length test
        let odd_data = [0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46, 0x40, 0x00, 0x40];
        let csum_odd = internet_checksum(&odd_data);
        assert_ne!(csum_odd, 0);
    }
}
