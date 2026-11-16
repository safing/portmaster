//! Conntrack cleanup over ctnetlink, port of Go `interception/nfq/conntrack.go`.
//!
//! Entries are dumped with a kernel-side mark filter and then deleted one by
//! one by echoing their original tuple, like go-conntrack does.

use std::io;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::os::fd::OwnedFd;

use rustix::net::{
    bind, netlink::SocketAddrNetlink, recv, sendto, socket, AddressFamily, RecvFlags, SendFlags,
    SocketType,
};

use crate::marks::Mark;

const NFNL_SUBSYS_CTNETLINK: u16 = 1;
const IPCTNL_MSG_CT_GET: u16 = 1;
const IPCTNL_MSG_CT_DELETE: u16 = 2;
const NLM_F_REQUEST: u16 = 0x1;
const NLM_F_ACK: u16 = 0x4;
const NLM_F_DUMP: u16 = 0x300;
const NLMSG_ERROR: u16 = 2;
const NLMSG_DONE: u16 = 3;
const NLA_F_NESTED: u16 = 0x8000;
const NLA_TYPE_MASK: u16 = 0x3fff;

const CTA_TUPLE_ORIG: u16 = 1;
const CTA_TUPLE_REPLY: u16 = 2;
const CTA_MARK: u16 = 8;
const CTA_MARK_MASK: u16 = 21;
const CTA_TUPLE_IP: u16 = 1;
const CTA_TUPLE_PROTO: u16 = 2;
const CTA_PROTO_NUM: u16 = 1;
const CTA_PROTO_SRC_PORT: u16 = 2;
const CTA_PROTO_DST_PORT: u16 = 3;
const CTA_IP_V4_SRC: u16 = 1;
const CTA_IP_V4_DST: u16 = 2;
const CTA_IP_V6_SRC: u16 = 3;
const CTA_IP_V6_DST: u16 = 4;

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

impl Family {
    fn af(self) -> u8 {
        match self {
            Family::V4 => 2,
            Family::V6 => 10,
        }
    }
}

/// One conntrack entry as needed for deletion.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    /// The raw CTA_TUPLE_ORIG attribute (header included), echoed on delete.
    orig_tuple: Vec<u8>,
    pub addrs: Vec<IpAddr>,
    pub mark: u32,
}

impl Entry {
    /// Go `isLoopbackConnection`: any address of either tuple is loopback.
    pub fn is_loopback(&self) -> bool {
        self.addrs.iter().any(IpAddr::is_loopback)
    }
}

/// The original tuple of a connection, as Go `DeleteMarkedConnection` builds it.
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

    /// The `CTA_TUPLE_ORIG` attribute. Ports are only sent for TCP and UDP.
    fn attr(&self) -> Vec<u8> {
        let mut ip = Vec::new();
        match (self.src, self.dst) {
            (IpAddr::V4(src), IpAddr::V4(dst)) => {
                put_attr(&mut ip, CTA_IP_V4_SRC, &src.octets());
                put_attr(&mut ip, CTA_IP_V4_DST, &dst.octets());
            }
            (src, dst) => {
                let to_v6 = |ip: IpAddr| match ip {
                    IpAddr::V6(ip) => ip,
                    IpAddr::V4(ip) => ip.to_ipv6_mapped(),
                };
                put_attr(&mut ip, CTA_IP_V6_SRC, &to_v6(src).octets());
                put_attr(&mut ip, CTA_IP_V6_DST, &to_v6(dst).octets());
            }
        }
        let mut proto = Vec::new();
        put_attr(&mut proto, CTA_PROTO_NUM, &[self.protocol]);
        if self.protocol == 6 || self.protocol == 17 {
            put_attr(&mut proto, CTA_PROTO_SRC_PORT, &self.src_port.to_be_bytes());
            put_attr(&mut proto, CTA_PROTO_DST_PORT, &self.dst_port.to_be_bytes());
        }
        let mut inner = Vec::new();
        put_attr(&mut inner, CTA_TUPLE_IP | NLA_F_NESTED, &ip);
        put_attr(&mut inner, CTA_TUPLE_PROTO | NLA_F_NESTED, &proto);
        let mut out = Vec::new();
        put_attr(&mut out, CTA_TUPLE_ORIG | NLA_F_NESTED, &inner);
        out
    }
}

/// A ctnetlink socket.
pub struct Conntrack {
    fd: OwnedFd,
    seq: u32,
}

impl Conntrack {
    /// Opens a NETLINK_NETFILTER socket (needs CAP_NET_ADMIN).
    pub fn open() -> io::Result<Conntrack> {
        let fd = socket(
            AddressFamily::NETLINK,
            SocketType::RAW,
            Some(rustix::net::netlink::NETFILTER),
        )?;
        bind(&fd, &SocketAddrNetlink::new(0, 0))?;
        Ok(Conntrack { fd, seq: 0 })
    }

    /// Dumps entries whose mark equals `mark`.
    pub fn query_mark(&mut self, family: Family, mark: u32) -> io::Result<Vec<Entry>> {
        let mut attrs = Vec::new();
        put_attr(&mut attrs, CTA_MARK, &mark.to_be_bytes());
        put_attr(&mut attrs, CTA_MARK_MASK, &u32::MAX.to_be_bytes());
        let msg = self.message(
            IPCTNL_MSG_CT_GET,
            NLM_F_REQUEST | NLM_F_DUMP,
            family,
            &attrs,
        );
        self.send(&msg)?;

        let mut entries = Vec::new();
        let mut buf = vec![0u8; 1 << 16];
        loop {
            let (n, _) = recv(&self.fd, &mut buf[..], RecvFlags::empty())?;
            let mut done = false;
            for (kind, _seq, payload) in messages(&buf[..n]) {
                match kind {
                    NLMSG_DONE => done = true,
                    NLMSG_ERROR => check_error(payload)?,
                    _ => {
                        if let Some(entry) = parse_entry(payload) {
                            // Filter again in case the kernel ignored the mark filter.
                            if entry.mark == mark {
                                entries.push(entry);
                            }
                        }
                    }
                }
            }
            if done {
                return Ok(entries);
            }
        }
    }

    /// Deletes the entry with the given original tuple (Go
    /// `DeleteMarkedConnection`); a missing entry is an `ENOENT` error.
    pub fn delete_tuple(&mut self, tuple: &ConnTuple) -> io::Result<()> {
        let msg = self.message(
            IPCTNL_MSG_CT_DELETE,
            NLM_F_REQUEST | NLM_F_ACK,
            tuple.family(),
            &tuple.attr(),
        );
        self.send(&msg)?;
        self.wait_ack()
    }

    /// Deletes one entry by its original tuple.
    pub fn delete(&mut self, family: Family, entry: &Entry) -> io::Result<()> {
        let msg = self.message(
            IPCTNL_MSG_CT_DELETE,
            NLM_F_REQUEST | NLM_F_ACK,
            family,
            &entry.orig_tuple,
        );
        self.send(&msg)?;
        self.wait_ack()
    }

    /// Go `deleteMarkedConnections`: removes entries with permanent verdict marks.
    pub fn delete_marked(&mut self, family: Family) -> (usize, Option<io::Error>) {
        let mut deleted = 0;
        let mut last_err = None;
        for mark in PERMANENT_MARKS {
            match self.query_mark(family, mark as u32) {
                Ok(entries) => {
                    for entry in entries {
                        match self.delete(family, &entry) {
                            Ok(()) => deleted += 1,
                            Err(e) => last_err = Some(e),
                        }
                    }
                }
                Err(e) => tracing::warn!(error = %e, "nfq: error on conntrack query"),
            }
        }
        (deleted, last_err)
    }

    /// Go `deleteUnmarkedConnections`: removes non-loopback entries with mark 0
    /// so that existing flows are re-evaluated (this disconnects them).
    pub fn delete_unmarked(&mut self, family: Family) -> (usize, Option<io::Error>) {
        let entries = match self.query_mark(family, 0) {
            Ok(entries) => entries,
            Err(e) => {
                tracing::warn!(error = %e, "nfq: error querying unmarked conntrack entries");
                return (0, None);
            }
        };
        let mut deleted = 0;
        let mut last_err = None;
        for entry in entries.iter().filter(|e| !e.is_loopback()) {
            match self.delete(family, entry) {
                Ok(()) => deleted += 1,
                Err(e) => last_err = Some(e),
            }
        }
        (deleted, last_err)
    }

    fn message(&mut self, msg_type: u16, flags: u16, family: Family, attrs: &[u8]) -> Vec<u8> {
        self.seq = self.seq.wrapping_add(1);
        let len = 16 + 4 + attrs.len();
        let mut m = Vec::with_capacity(len);
        m.extend_from_slice(&(len as u32).to_ne_bytes());
        m.extend_from_slice(&((NFNL_SUBSYS_CTNETLINK << 8) | msg_type).to_ne_bytes());
        m.extend_from_slice(&flags.to_ne_bytes());
        m.extend_from_slice(&self.seq.to_ne_bytes());
        m.extend_from_slice(&0u32.to_ne_bytes());
        // nfgenmsg: family, version 0, res_id 0.
        m.extend_from_slice(&[family.af(), 0, 0, 0]);
        m.extend_from_slice(attrs);
        m
    }

    /// Waits for the ACK of the last sent message; replies to older requests
    /// (e.g. leftovers of an aborted dump) are skipped.
    fn wait_ack(&mut self) -> io::Result<()> {
        let mut buf = vec![0u8; 8192];
        loop {
            let (n, _) = recv(&self.fd, &mut buf[..], RecvFlags::empty())?;
            for (kind, seq, payload) in messages(&buf[..n]) {
                if kind == NLMSG_ERROR && seq == self.seq {
                    return check_error(payload);
                }
            }
        }
    }

    fn send(&self, msg: &[u8]) -> io::Result<()> {
        sendto(
            &self.fd,
            msg,
            SendFlags::empty(),
            &SocketAddrNetlink::new(0, 0),
        )?;
        Ok(())
    }
}

fn align4(n: usize) -> usize {
    (n + 3) & !3
}

fn put_attr(out: &mut Vec<u8>, kind: u16, value: &[u8]) {
    let len = 4 + value.len();
    out.extend_from_slice(&(len as u16).to_ne_bytes());
    out.extend_from_slice(&kind.to_ne_bytes());
    out.extend_from_slice(value);
    out.resize(out.len() + align4(len) - len, 0);
}

/// Iterates netlink messages: (type, sequence number, payload after the 16-byte header).
fn messages(mut buf: &[u8]) -> Vec<(u16, u32, &[u8])> {
    let mut out = Vec::new();
    while buf.len() >= 16 {
        let len = u32::from_ne_bytes([buf[0], buf[1], buf[2], buf[3]]) as usize;
        if len < 16 || len > buf.len() {
            break;
        }
        let kind = u16::from_ne_bytes([buf[4], buf[5]]);
        let seq = u32::from_ne_bytes([buf[8], buf[9], buf[10], buf[11]]);
        out.push((kind, seq, &buf[16..len]));
        buf = &buf[align4(len).min(buf.len())..];
    }
    out
}

/// Iterates attributes: (type without flags, value, raw attribute bytes).
fn attrs(mut buf: &[u8]) -> Vec<(u16, &[u8], &[u8])> {
    let mut out = Vec::new();
    while buf.len() >= 4 {
        let len = usize::from(u16::from_ne_bytes([buf[0], buf[1]]));
        if len < 4 || len > buf.len() {
            break;
        }
        let kind = u16::from_ne_bytes([buf[2], buf[3]]) & NLA_TYPE_MASK;
        out.push((kind, &buf[4..len], &buf[..len]));
        buf = &buf[align4(len).min(buf.len())..];
    }
    out
}

fn check_error(payload: &[u8]) -> io::Result<()> {
    let code = payload
        .get(..4)
        .map(|b| i32::from_ne_bytes([b[0], b[1], b[2], b[3]]))
        .ok_or_else(|| io::Error::other("short netlink error message"))?;
    if code == 0 {
        Ok(())
    } else {
        Err(io::Error::from_raw_os_error(-code))
    }
}

fn parse_entry(payload: &[u8]) -> Option<Entry> {
    // Skip nfgenmsg.
    let body = payload.get(4..)?;
    let mut orig_tuple = None;
    let mut addrs = Vec::new();
    let mut mark = 0;
    for (kind, value, raw) in attrs(body) {
        match kind {
            CTA_TUPLE_ORIG | CTA_TUPLE_REPLY => {
                if kind == CTA_TUPLE_ORIG {
                    // Echo it as a nested attribute.
                    let mut tuple = raw.to_vec();
                    let t = (CTA_TUPLE_ORIG | NLA_F_NESTED).to_ne_bytes();
                    tuple[2..4].copy_from_slice(&t);
                    orig_tuple = Some(tuple);
                }
                tuple_addrs(value, &mut addrs);
            }
            CTA_MARK => {
                if let Ok(b) = <[u8; 4]>::try_from(value) {
                    mark = u32::from_be_bytes(b);
                }
            }
            _ => {}
        }
    }
    let mut orig_tuple = orig_tuple?;
    orig_tuple.resize(align4(orig_tuple.len()), 0);
    Some(Entry {
        orig_tuple,
        addrs,
        mark,
    })
}

fn tuple_addrs(tuple: &[u8], out: &mut Vec<IpAddr>) {
    for (kind, value, _) in attrs(tuple) {
        if kind != CTA_TUPLE_IP {
            continue;
        }
        for (ip_kind, ip, _) in attrs(value) {
            match ip_kind {
                CTA_IP_V4_SRC | CTA_IP_V4_DST => {
                    if let Ok(b) = <[u8; 4]>::try_from(ip) {
                        out.push(IpAddr::V4(Ipv4Addr::from(b)));
                    }
                }
                CTA_IP_V6_SRC | CTA_IP_V6_DST => {
                    if let Ok(b) = <[u8; 16]>::try_from(ip) {
                        out.push(IpAddr::V6(Ipv6Addr::from(b)));
                    }
                }
                _ => {}
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn nested(kind: u16, children: &[u8]) -> Vec<u8> {
        let mut out = Vec::new();
        put_attr(&mut out, kind | NLA_F_NESTED, children);
        out
    }

    fn tuple(kind: u16, src: [u8; 4], dst: [u8; 4]) -> Vec<u8> {
        let mut ip = Vec::new();
        put_attr(&mut ip, CTA_IP_V4_SRC, &src);
        put_attr(&mut ip, CTA_IP_V4_DST, &dst);
        let ip = nested(CTA_TUPLE_IP, &ip);
        nested(kind, &ip)
    }

    fn entry_payload(src: [u8; 4], dst: [u8; 4], mark: u32) -> Vec<u8> {
        let mut p = vec![2, 0, 0, 0];
        p.extend(tuple(CTA_TUPLE_ORIG, src, dst));
        p.extend(tuple(CTA_TUPLE_REPLY, dst, src));
        put_attr(&mut p, CTA_MARK, &mark.to_be_bytes());
        p
    }

    #[test]
    fn parses_dump_entries_and_loopback() {
        let e = parse_entry(&entry_payload([10, 0, 0, 2], [1, 1, 1, 1], 1710)).unwrap();
        assert_eq!(e.mark, 1710);
        assert_eq!(e.addrs.len(), 4);
        assert!(!e.is_loopback());
        // Echoed tuple is nested CTA_TUPLE_ORIG.
        assert_eq!(
            u16::from_ne_bytes([e.orig_tuple[2], e.orig_tuple[3]]),
            CTA_TUPLE_ORIG | NLA_F_NESTED
        );

        let lo = parse_entry(&entry_payload([127, 0, 0, 1], [127, 0, 0, 1], 0)).unwrap();
        assert!(lo.is_loopback());
    }

    #[test]
    fn builds_tuple_attribute() {
        let tuple = ConnTuple {
            src: "10.0.0.2".parse().unwrap(),
            dst: "1.1.1.1".parse().unwrap(),
            protocol: 6,
            src_port: 40000,
            dst_port: 443,
        };
        assert_eq!(tuple.family(), Family::V4);
        let attr = tuple.attr();
        let outer = attrs(&attr);
        assert_eq!(outer.len(), 1);
        assert_eq!(outer[0].0, CTA_TUPLE_ORIG);
        let children = attrs(outer[0].1);
        assert_eq!(children[0].0, CTA_TUPLE_IP);
        assert_eq!(children[1].0, CTA_TUPLE_PROTO);
        let proto = attrs(children[1].1);
        assert_eq!(proto[0].1, &[6]);
        assert_eq!(proto[1].1, &40000u16.to_be_bytes());
        assert_eq!(proto[2].1, &443u16.to_be_bytes());

        let icmp = ConnTuple {
            protocol: 1,
            ..tuple
        };
        let attr = icmp.attr();
        let inner = attrs(attrs(&attr)[0].1);
        assert_eq!(attrs(inner[1].1).len(), 1);
    }

    #[test]
    fn builds_dump_request_with_mark_filter() {
        let mut ct = Conntrack {
            fd: rustix::fs::open(
                "/dev/null",
                rustix::fs::OFlags::RDONLY,
                rustix::fs::Mode::empty(),
            )
            .unwrap(),
            seq: 0,
        };
        let mut a = Vec::new();
        put_attr(&mut a, CTA_MARK, &1710u32.to_be_bytes());
        let m = ct.message(
            IPCTNL_MSG_CT_GET,
            NLM_F_REQUEST | NLM_F_DUMP,
            Family::V6,
            &a,
        );
        assert_eq!(
            u32::from_ne_bytes(m[0..4].try_into().unwrap()) as usize,
            m.len()
        );
        assert_eq!(u16::from_ne_bytes([m[4], m[5]]), 0x0101);
        assert_eq!(m[16], 10);
        assert_eq!(&m[24..28], &1710u32.to_be_bytes());
    }

    #[test]
    fn netlink_errors_map_to_io_errors() {
        assert!(check_error(&0i32.to_ne_bytes()).is_ok());
        let err = check_error(&(-2i32).to_ne_bytes()).unwrap_err();
        assert_eq!(err.raw_os_error(), Some(2));
        assert!(check_error(&[1]).is_err());
        assert!(messages(&[1, 2, 3]).is_empty());
        let mut m = vec![0u8; 20];
        m[0..4].copy_from_slice(&20u32.to_ne_bytes());
        m[4..6].copy_from_slice(&NLMSG_ERROR.to_ne_bytes());
        m[8..12].copy_from_slice(&42u32.to_ne_bytes());
        assert_eq!(messages(&m), vec![(NLMSG_ERROR, 42, &m[16..])]);
    }

    #[test]
    #[ignore = "needs CAP_NET_ADMIN"]
    fn live_query_marked() {
        let mut ct = Conntrack::open().unwrap();
        let entries = ct.query_mark(Family::V4, 0).unwrap();
        assert!(entries.iter().all(|e| e.mark == 0));
    }
}
