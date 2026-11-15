#![forbid(unsafe_code)]

//! # pm-dns
//!
//! Fast, lightweight DNS wire protocol parsing and sinkhole / blocking response generation
//! matching Portmaster's `service/nameserver/nsutil` semantics.

use std::net::{Ipv4Addr, Ipv6Addr};

/// DNS Record Types
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum QType {
    A,
    AAAA,
    PTR,
    CNAME,
    TXT,
    Unknown(u16),
}

impl From<u16> for QType {
    fn from(val: u16) -> Self {
        match val {
            1 => Self::A,
            28 => Self::AAAA,
            12 => Self::PTR,
            5 => Self::CNAME,
            16 => Self::TXT,
            other => Self::Unknown(other),
        }
    }
}

impl From<QType> for u16 {
    fn from(val: QType) -> Self {
        match val {
            QType::A => 1,
            QType::AAAA => 28,
            QType::PTR => 12,
            QType::CNAME => 5,
            QType::TXT => 16,
            QType::Unknown(code) => code,
        }
    }
}

/// DNS Response Codes
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RCode {
    NoError = 0,
    FormatError = 1,
    ServerFailure = 2,
    NXDomain = 3,
    Refused = 5,
}

/// Parsed DNS Question
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DnsQuestion {
    pub name: String,
    pub qtype: QType,
    pub qclass: u16,
}

/// Minimal parsed DNS Query Header and Question
#[derive(Debug, Clone)]
pub struct DnsQuery {
    pub id: u16,
    pub flags: u16,
    pub question: Option<DnsQuestion>,
}

impl DnsQuery {
    /// Parse the header and first question from wire-format DNS packet bytes.
    pub fn parse(buf: &[u8]) -> Option<Self> {
        if buf.len() < 12 {
            return None;
        }

        let id = u16::from_be_bytes([buf[0], buf[1]]);
        let flags = u16::from_be_bytes([buf[2], buf[3]]);
        let qdcount = u16::from_be_bytes([buf[4], buf[5]]);

        if qdcount == 0 {
            return Some(Self { id, flags, question: None });
        }

        // Parse Question Name
        let mut pos = 12;
        let mut name_parts = Vec::new();

        while pos < buf.len() {
            let len = buf[pos] as usize;
            if len == 0 {
                pos += 1;
                break;
            }
            if pos + 1 + len > buf.len() {
                return None;
            }
            let part = std::str::from_utf8(&buf[pos + 1..pos + 1 + len]).ok()?;
            name_parts.push(part);
            pos += 1 + len;
        }

        if pos + 4 > buf.len() {
            return None;
        }

        let qtype = u16::from_be_bytes([buf[pos], buf[pos + 1]]).into();
        let qclass = u16::from_be_bytes([buf[pos + 2], buf[pos + 3]]);

        let name = name_parts.join(".") + ".";

        Some(Self {
            id,
            flags,
            question: Some(DnsQuestion { name, qtype, qclass }),
        })
    }
}

/// Response action for sinkholing/blocking.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BlockAction {
    BlockIP,
    ZeroIP,
    Localhost,
    NXDomain,
}

impl BlockAction {
    /// Generate a synthetic DNS wire response for a blocked domain.
    pub fn create_response(&self, query: &DnsQuery, raw_query: &[u8]) -> Vec<u8> {
        let mut resp = Vec::with_capacity(512);

        // Copy transaction ID
        resp.extend_from_slice(&query.id.to_be_bytes());

        let is_nxdomain = *self == Self::NXDomain;
        let rcode = if is_nxdomain { RCode::NXDomain as u16 } else { RCode::NoError as u16 };

        // Flags: QR=1 (response), RD preserved, RA=1
        let flags = 0x8180 | rcode;
        resp.extend_from_slice(&flags.to_be_bytes());

        // Question count = 1
        resp.extend_from_slice(&1u16.to_be_bytes());

        let has_answer = !is_nxdomain && query.question.as_ref().map_or(false, |q| {
            matches!(q.qtype, QType::A | QType::AAAA)
        });

        // Answer count
        let ancount: u16 = if has_answer { 1 } else { 0 };
        resp.extend_from_slice(&ancount.to_be_bytes());

        // NS count = 0, AR count = 0
        resp.extend_from_slice(&0u16.to_be_bytes());
        resp.extend_from_slice(&0u16.to_be_bytes());

        // Copy question section from original query if available
        if raw_query.len() >= 12 {
            let mut pos = 12;
            while pos < raw_query.len() {
                let len = raw_query[pos] as usize;
                if len == 0 {
                    pos += 1;
                    break;
                }
                pos += 1 + len;
            }
            if pos + 4 <= raw_query.len() {
                resp.extend_from_slice(&raw_query[12..pos + 4]);
            }
        }

        // Add Answer RR
        if has_answer {
            if let Some(ref q) = query.question {
                // Name pointer to offset 12 (0xc00c)
                resp.extend_from_slice(&[0xc0, 0x0c]);

                let qtype_code: u16 = q.qtype.into();
                resp.extend_from_slice(&qtype_code.to_be_bytes());
                resp.extend_from_slice(&1u16.to_be_bytes()); // Class IN
                resp.extend_from_slice(&1u32.to_be_bytes()); // TTL 1s

                match q.qtype {
                    QType::A => {
                        resp.extend_from_slice(&4u16.to_be_bytes()); // RDLength 4
                        let ip = match self {
                            Self::BlockIP => Ipv4Addr::new(0, 0, 0, 17),
                            Self::ZeroIP => Ipv4Addr::new(0, 0, 0, 0),
                            Self::Localhost => Ipv4Addr::new(127, 0, 0, 1),
                            Self::NXDomain => unreachable!(),
                        };
                        resp.extend_from_slice(&ip.octets());
                    }
                    QType::AAAA => {
                        resp.extend_from_slice(&16u16.to_be_bytes()); // RDLength 16
                        let ip = match self {
                            Self::BlockIP => "::17".parse::<Ipv6Addr>().unwrap(),
                            Self::ZeroIP => "::".parse::<Ipv6Addr>().unwrap(),
                            Self::Localhost => "::1".parse::<Ipv6Addr>().unwrap(),
                            Self::NXDomain => unreachable!(),
                        };
                        resp.extend_from_slice(&ip.octets());
                    }
                    _ => {}
                }
            }
        }

        resp
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_dns_query_parsing() {
        // Raw DNS packet for: google.com IN A, ID: 0x1234
        #[rustfmt::skip]
        let packet = [
            0x12, 0x34, // ID
            0x01, 0x00, // Flags (Standard query, RD=1)
            0x00, 0x01, // QDCOUNT=1
            0x00, 0x00, // ANCOUNT=0
            0x00, 0x00, // NSCOUNT=0
            0x00, 0x00, // ARCOUNT=0
            0x06, b'g', b'o', b'o', b'g', b'l', b'e',
            0x03, b'c', b'o', b'm',
            0x00,       // End of name
            0x00, 0x01, // Type A
            0x00, 0x01, // Class IN
        ];

        let query = DnsQuery::parse(&packet).unwrap();
        assert_eq!(query.id, 0x1234);
        let q = query.question.unwrap();
        assert_eq!(q.name, "google.com.");
        assert_eq!(q.qtype, QType::A);
        assert_eq!(q.qclass, 1);
    }

    #[test]
    fn test_block_ip_response() {
        #[rustfmt::skip]
        let packet = [
            0xab, 0xcd,
            0x01, 0x00,
            0x00, 0x01,
            0x00, 0x00,
            0x00, 0x00,
            0x00, 0x00,
            0x03, b'a', b'd', b's',
            0x00,
            0x00, 0x01, // Type A
            0x00, 0x01, // Class IN
        ];

        let query = DnsQuery::parse(&packet).unwrap();
        let resp = BlockAction::BlockIP.create_response(&query, &packet);

        assert_eq!(resp[0..2], [0xab, 0xcd]); // Preserves ID
        assert_eq!(resp[2] & 0x80, 0x80); // QR=1 (response)
        // Check answer IP is 0.0.0.17
        assert_eq!(&resp[resp.len() - 4..], &[0, 0, 0, 17]);
    }
}
