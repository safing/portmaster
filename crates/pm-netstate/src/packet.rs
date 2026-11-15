//! Minimal port of `service/network/packet.Info`, as used by the state lookups.

use std::net::IpAddr;
use std::time::SystemTime;

/// IP version of a packet.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IpVersion {
    Unknown,
    V4,
    V6,
}

/// Transport protocol of a packet.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IpProtocol {
    Unknown,
    TCP,
    UDP,
}

/// Information about a network packet.
#[derive(Debug, Clone)]
pub struct PacketInfo {
    pub inbound: bool,
    pub version: IpVersion,
    pub protocol: IpProtocol,
    pub src_port: u16,
    pub dst_port: u16,
    pub src: IpAddr,
    pub dst: IpAddr,
    pub seen_at: SystemTime,
}

impl PacketInfo {
    pub fn new(src: IpAddr, dst: IpAddr, protocol: IpProtocol, inbound: bool) -> PacketInfo {
        let version = match (src, dst) {
            (IpAddr::V4(_), _) => IpVersion::V4,
            (IpAddr::V6(_), _) => IpVersion::V6,
        };
        PacketInfo {
            inbound,
            version,
            protocol,
            src_port: 0,
            dst_port: 0,
            src,
            dst,
            seen_at: SystemTime::now(),
        }
    }

    pub fn local_ip(&self) -> IpAddr {
        if self.inbound {
            self.dst
        } else {
            self.src
        }
    }

    pub fn remote_ip(&self) -> IpAddr {
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
}
