//! Inert conntrack fallback for non-Linux platforms.
//!
//! Connection tracking verdict reset is a netlink feature of the Linux kernel
//! (Go `nfq.DeleteMarkedConnection`). Windows exposes an equivalent only
//! through the kernel extension, which is not part of this port. This module
//! keeps the Linux API surface available so that the firewall compiles, but
//! every operation that needs a kernel socket returns an error. Nothing here
//! touches the network.

use std::io;
use std::net::IpAddr;

use crate::marks::Mark;

/// Marks of permanent verdicts that are reset on (de)activation.
pub const PERMANENT_MARKS: [Mark; 6] = [
    Mark::AcceptAlways,
    Mark::BlockAlways,
    Mark::DropAlways,
    Mark::RerouteNs,
    Mark::RerouteSpn,
    Mark::RerouteSplitTun,
];

/// IP family of conntrack entries.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Family {
    V4,
    V6,
}

/// One conntrack entry as needed for deletion.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    pub addrs: Vec<IpAddr>,
    pub mark: u32,
}

impl Entry {
    /// Whether any address of the entry is loopback.
    pub fn is_loopback(&self) -> bool {
        self.addrs.iter().any(IpAddr::is_loopback)
    }
}

/// The original tuple of a connection.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ConnTuple {
    pub src: IpAddr,
    pub dst: IpAddr,
    pub protocol: u8,
    pub src_port: u16,
    pub dst_port: u16,
}

impl ConnTuple {
    pub fn family(&self) -> Family {
        if self.dst.is_ipv6() {
            Family::V6
        } else {
            Family::V4
        }
    }
}

/// Stand-in for a ctnetlink socket.
#[derive(Debug, Default, Clone, Copy)]
pub struct Conntrack;

fn unsupported() -> io::Error {
    io::Error::new(
        io::ErrorKind::Unsupported,
        "conntrack verdict reset is only available on Linux",
    )
}

impl Conntrack {
    /// Never succeeds off Linux: there is no ctnetlink socket to open.
    pub fn open() -> io::Result<Conntrack> {
        Err(unsupported())
    }

    pub fn query_mark(&mut self, _family: Family, _mark: u32) -> io::Result<Vec<Entry>> {
        Err(unsupported())
    }

    pub fn delete_tuple(&mut self, _tuple: &ConnTuple) -> io::Result<()> {
        Err(unsupported())
    }

    pub fn delete(&mut self, _family: Family, _entry: &Entry) -> io::Result<()> {
        Err(unsupported())
    }

    pub fn delete_marked(&mut self, _family: Family) -> (usize, Option<io::Error>) {
        (0, Some(unsupported()))
    }

    pub fn delete_unmarked(&mut self, _family: Family) -> (usize, Option<io::Error>) {
        (0, Some(unsupported()))
    }
}
