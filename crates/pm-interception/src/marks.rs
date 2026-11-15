//! Verdict marks, port of Go `interception/nfq/packet.go`.
//!
//! The kernel verdict is always ACCEPT; the mark tells the iptables
//! PORTMASTER-FILTER/REDIRECT chains what to do with the packet.

use std::net::IpAddr;

use pm_netstate::IpProtocol;

/// Packet marks understood by the Portmaster iptables rules.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u32)]
pub enum Mark {
    Accept = 1700,
    Block = 1701,
    Drop = 1702,
    /// Accept and finalize in iptables; only for Portmaster-owned outbound connections.
    AcceptFinal = 1709,
    AcceptAlways = 1710,
    BlockAlways = 1711,
    DropAlways = 1712,
    RerouteNs = 1799,
    RerouteSpn = 1717,
    RerouteSplitTun = 1719,
}

impl Mark {
    /// Go `markToString`.
    pub fn name(self) -> &'static str {
        match self {
            Mark::Accept => "Accept",
            Mark::Block => "Block",
            Mark::Drop => "Drop",
            Mark::AcceptFinal => "AcceptFinal",
            Mark::AcceptAlways => "AcceptAlways",
            Mark::BlockAlways => "BlockAlways",
            Mark::DropAlways => "DropAlways",
            Mark::RerouteNs => "RerouteNS",
            Mark::RerouteSpn => "RerouteSPN",
            Mark::RerouteSplitTun => "RerouteSplitTun",
        }
    }
}

/// A firewall decision for a packet (the Go `Packet` verdict methods).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Verdict {
    Accept,
    Block,
    Drop,
    PermanentAccept,
    PermanentAcceptFinal,
    PermanentBlock,
    PermanentDrop,
    RerouteToNameserver,
    RerouteToTunnel,
    RerouteToSplitTun,
}

/// What the verdict needs to know about the packet.
#[derive(Debug, Clone, Copy)]
pub struct PacketFacts {
    pub inbound: bool,
    pub protocol: IpProtocol,
    /// Whether the protocol is ICMPv6 (Go distinguishes ICMP and ICMPv6 here).
    pub icmpv6: bool,
    pub dst: IpAddr,
}

impl Verdict {
    /// Returns the mark to set, with the Go special cases.
    pub fn mark(self, pkt: &PacketFacts) -> Mark {
        let icmpv4 = pkt.protocol == IpProtocol::Icmp && !pkt.icmpv6;
        let any_icmp = pkt.protocol == IpProtocol::Icmp;
        // A sticky connmark on outbound loopback would skip the INPUT queue.
        let outbound_loopback = !pkt.inbound && pkt.dst.is_loopback();
        match self {
            Verdict::Accept => Mark::Accept,
            // Go only special-cases ICMPv4 here, ICMPv6 is handled by the iptables rule.
            Verdict::Block if icmpv4 => Mark::Drop,
            Verdict::Block => Mark::Block,
            Verdict::Drop => Mark::Drop,
            Verdict::PermanentAccept if outbound_loopback => Mark::Accept,
            Verdict::PermanentAccept => Mark::AcceptAlways,
            Verdict::PermanentAcceptFinal if outbound_loopback => Mark::Accept,
            Verdict::PermanentAcceptFinal => Mark::AcceptFinal,
            Verdict::PermanentBlock if any_icmp => Mark::DropAlways,
            Verdict::PermanentBlock => Mark::BlockAlways,
            Verdict::PermanentDrop => Mark::DropAlways,
            Verdict::RerouteToNameserver => Mark::RerouteNs,
            Verdict::RerouteToTunnel => Mark::RerouteSpn,
            Verdict::RerouteToSplitTun => Mark::RerouteSplitTun,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn facts(inbound: bool, protocol: IpProtocol, icmpv6: bool, dst: &str) -> PacketFacts {
        PacketFacts {
            inbound,
            protocol,
            icmpv6,
            dst: dst.parse().unwrap(),
        }
    }

    #[test]
    fn marks_follow_go_special_cases() {
        let tcp = facts(false, IpProtocol::TCP, false, "1.1.1.1");
        let icmp4 = facts(false, IpProtocol::Icmp, false, "1.1.1.1");
        let icmp6 = facts(false, IpProtocol::Icmp, true, "2001:db8::1");
        let lo = facts(false, IpProtocol::TCP, false, "127.0.0.1");
        let lo_in = facts(true, IpProtocol::TCP, false, "127.0.0.1");

        assert_eq!(Verdict::Block.mark(&tcp), Mark::Block);
        assert_eq!(Verdict::Block.mark(&icmp4), Mark::Drop);
        assert_eq!(Verdict::Block.mark(&icmp6), Mark::Block);
        assert_eq!(Verdict::PermanentBlock.mark(&icmp6), Mark::DropAlways);
        assert_eq!(Verdict::PermanentBlock.mark(&tcp), Mark::BlockAlways);
        assert_eq!(Verdict::PermanentAccept.mark(&tcp), Mark::AcceptAlways);
        assert_eq!(Verdict::PermanentAccept.mark(&lo), Mark::Accept);
        assert_eq!(Verdict::PermanentAccept.mark(&lo_in), Mark::AcceptAlways);
        assert_eq!(Verdict::PermanentAcceptFinal.mark(&lo), Mark::Accept);
        assert_eq!(Verdict::RerouteToNameserver.mark(&tcp) as u32, 1799);
        assert_eq!(Verdict::RerouteToSplitTun.mark(&tcp) as u32, 1719);
    }
}
