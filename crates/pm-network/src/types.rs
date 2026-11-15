//! Basic network types: verdicts, connection types, packet info and IDs.

use std::fmt;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::time::SystemTime;

use serde::{Serialize, Serializer};

/// IP protocol numbers, as in Go `packet.IPProtocol`.
pub mod protocol {
    pub use pm_interception::packet::{
        PROTO_ICMP as ICMP, PROTO_ICMPV6 as ICMPV6, PROTO_IGMP as IGMP, PROTO_TCP as TCP,
        PROTO_UDP as UDP,
    };
    pub const UDP_LITE: u8 = 136;
    pub const RAW: u8 = 255;
}

/// IP versions, as in Go `packet.IPVersion`.
pub mod version {
    pub const V4: u8 = 4;
    pub const V6: u8 = 6;
}

/// Non-domain connection scopes.
pub const INCOMING_HOST: &str = "IH";
pub const INCOMING_LAN: &str = "IL";
pub const INCOMING_INTERNET: &str = "II";
pub const INCOMING_INVALID: &str = "IX";
pub const PEER_HOST: &str = "PH";
pub const PEER_LAN: &str = "PL";
pub const PEER_INTERNET: &str = "PI";
pub const PEER_INVALID: &str = "PX";

/// The decision made about a connection. Values and order match Go.
///
/// The derived ordering follows the numeric values; the firewall uses it to
/// never weaken a verdict.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
#[repr(i8)]
pub enum Verdict {
    #[default]
    Undecided = 0,
    Undeterminable = 1,
    Accept = 2,
    Block = 3,
    Drop = 4,
    RerouteToNameserver = 5,
    RerouteToTunnel = 6,
    Failed = 7,
    RerouteToSplitTun = 8,
}

impl Verdict {
    /// Returns the verdict for a numeric value.
    pub fn from_i8(value: i8) -> Option<Verdict> {
        Some(match value {
            0 => Verdict::Undecided,
            1 => Verdict::Undeterminable,
            2 => Verdict::Accept,
            3 => Verdict::Block,
            4 => Verdict::Drop,
            5 => Verdict::RerouteToNameserver,
            6 => Verdict::RerouteToTunnel,
            7 => Verdict::Failed,
            8 => Verdict::RerouteToSplitTun,
            _ => return None,
        })
    }

    /// Returns the verdict as a past tense verb.
    pub fn verb(self) -> &'static str {
        match self {
            Verdict::Undecided => "undecided",
            Verdict::Undeterminable => "undeterminable",
            Verdict::Accept => "accepted",
            Verdict::Block => "blocked",
            Verdict::Drop => "dropped",
            Verdict::RerouteToNameserver => "redirected to nameserver",
            Verdict::RerouteToTunnel => "tunneled",
            Verdict::RerouteToSplitTun => "split tunneled",
            Verdict::Failed => "failed",
        }
    }
}

impl fmt::Display for Verdict {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Verdict::Undecided => "<Undecided>",
            Verdict::Undeterminable => "<Undeterminable>",
            Verdict::Accept => "Accept",
            Verdict::Block => "Block",
            Verdict::Drop => "Drop",
            Verdict::RerouteToNameserver => "RerouteToNameserver",
            Verdict::RerouteToTunnel => "RerouteToTunnel",
            Verdict::RerouteToSplitTun => "RerouteToSplitTun",
            Verdict::Failed => "Failed",
        })
    }
}

impl Serialize for Verdict {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_i8(*self as i8)
    }
}

/// The type of a connection.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
#[repr(i8)]
pub enum ConnectionType {
    #[default]
    Undefined = 0,
    Ip = 1,
    Dns = 2,
}

impl Serialize for ConnectionType {
    fn serialize<S: Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        serializer.serialize_i8(*self as i8)
    }
}

/// Converts IPv4-mapped IPv6 addresses to IPv4, like Go's `net.IP.To4`.
pub fn canonical_ip(ip: IpAddr) -> IpAddr {
    ip.to_canonical()
}

/// Formats an IP like Go `net.IP.String`; a missing IP is `<nil>`.
pub fn format_ip(ip: Option<IpAddr>) -> String {
    match ip {
        Some(ip) => canonical_ip(ip).to_string(),
        None => "<nil>".to_string(),
    }
}

/// Information about a packet, a port of Go `packet.Info`.
///
/// Unlike `pm_netstate::PacketInfo` it keeps the raw protocol number and
/// allows missing addresses, which are both needed for connection IDs.
#[derive(Debug, Clone)]
pub struct PacketInfo {
    pub inbound: bool,
    pub in_tunnel: bool,
    pub version: u8,
    pub protocol: u8,
    pub src_port: u16,
    pub dst_port: u16,
    pub src: Option<IpAddr>,
    pub dst: Option<IpAddr>,
    pub pid: i32,
    pub seen_at: SystemTime,
}

impl PacketInfo {
    /// Creates packet info with undefined PID and the current time.
    pub fn new(
        protocol: u8,
        src: IpAddr,
        src_port: u16,
        dst: IpAddr,
        dst_port: u16,
        inbound: bool,
    ) -> PacketInfo {
        PacketInfo {
            inbound,
            in_tunnel: false,
            version: if canonical_ip(src).is_ipv4() {
                version::V4
            } else {
                version::V6
            },
            protocol,
            src_port,
            dst_port,
            src: Some(src),
            dst: Some(dst),
            pid: pm_netstate::UNDEFINED_PROCESS_ID,
            seen_at: SystemTime::now(),
        }
    }

    pub fn local_ip(&self) -> Option<IpAddr> {
        if self.inbound {
            self.dst
        } else {
            self.src
        }
    }

    pub fn remote_ip(&self) -> Option<IpAddr> {
        if self.inbound {
            self.src
        } else {
            self.dst
        }
    }

    pub fn local_port(&self) -> u16 {
        if self.inbound {
            self.dst_port
        } else {
            self.src_port
        }
    }

    pub fn remote_port(&self) -> u16 {
        if self.inbound {
            self.src_port
        } else {
            self.dst_port
        }
    }

    /// Creates the connection ID of the packet.
    pub fn connection_id(&self) -> String {
        create_connection_id(
            self.protocol,
            self.src,
            self.src_port,
            self.dst,
            self.dst_port,
            self.inbound,
        )
    }

    /// Converts to the packet info used by the state lookups.
    pub fn to_netstate(&self) -> pm_netstate::PacketInfo {
        let unspecified = |version: u8| {
            if version == version::V6 {
                IpAddr::V6(Ipv6Addr::UNSPECIFIED)
            } else {
                IpAddr::V4(Ipv4Addr::UNSPECIFIED)
            }
        };
        let protocol = match self.protocol {
            protocol::TCP => pm_netstate::IpProtocol::TCP,
            protocol::UDP => pm_netstate::IpProtocol::UDP,
            protocol::ICMP | protocol::ICMPV6 => pm_netstate::IpProtocol::Icmp,
            _ => pm_netstate::IpProtocol::Unknown,
        };
        let mut info = pm_netstate::PacketInfo::new(
            self.src.unwrap_or_else(|| unspecified(self.version)),
            self.dst.unwrap_or_else(|| unspecified(self.version)),
            protocol,
            self.inbound,
        );
        info.version = match self.version {
            version::V4 => pm_netstate::IpVersion::V4,
            version::V6 => pm_netstate::IpVersion::V6,
            _ => pm_netstate::IpVersion::Unknown,
        };
        info.src_port = self.src_port;
        info.dst_port = self.dst_port;
        info.pid = self.pid;
        info.seen_at = self.seen_at;
        info
    }
}

impl From<&pm_interception::packet::Parsed> for PacketInfo {
    /// Converts parsed headers of an intercepted packet. The direction is not
    /// part of the headers: the result is outbound and the caller sets
    /// `inbound` from the packet mark.
    fn from(parsed: &pm_interception::packet::Parsed) -> PacketInfo {
        let mut info = PacketInfo::new(
            parsed.protocol,
            parsed.src,
            parsed.src_port,
            parsed.dst,
            parsed.dst_port,
            false,
        );
        info.version = match parsed.version {
            pm_netstate::IpVersion::V4 => version::V4,
            pm_netstate::IpVersion::V6 => version::V6,
            pm_netstate::IpVersion::Unknown => 0,
        };
        info
    }
}

/// Creates a connection ID, like Go `packet.CreateConnectionID`.
///
/// The ID does not depend on the packet direction: the local side comes first.
pub fn create_connection_id(
    protocol: u8,
    src: Option<IpAddr>,
    src_port: u16,
    dst: Option<IpAddr>,
    dst_port: u16,
    inbound: bool,
) -> String {
    let (src_s, dst_s) = (format_ip(src), format_ip(dst));
    if protocol == protocol::TCP || protocol == protocol::UDP {
        if inbound {
            return format!("{protocol}-{dst_s}-{dst_port}-{src_s}-{src_port}");
        }
        return format!("{protocol}-{src_s}-{src_port}-{dst_s}-{dst_port}");
    }
    if inbound {
        return format!("{protocol}-{dst_s}-{src_s}");
    }
    format!("{protocol}-{src_s}-{dst_s}")
}
