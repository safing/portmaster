//! The packet abstraction of the handler, a port of what Go `packet.Packet`
//! offers to `packet_handler.go`.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex;

use pm_interception::marks::PacketFacts;
use pm_interception::packet::{icmp_type, PROTO_ICMPV6};
use pm_interception::queue::{QueuedPacket as RawPacket, VerdictHandle};
use pm_interception::Verdict;
use pm_network::PacketInfo;

/// A verdict could not be applied to the packet.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum VerdictError {
    #[error("packet already has a verdict")]
    AlreadySet,
    #[error("{0}")]
    Other(String),
}

/// Result of reading the ICMP header of a packet (Go `LoadPacketData` and the
/// ICMP layer lookup).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IcmpType {
    /// The packet data could not be loaded.
    LoadFailed,
    /// The data loaded, but has no ICMP layer.
    NoLayer,
    Type(u8),
}

/// What the handler needs from an intercepted packet.
///
/// Go has one method per verdict (`Accept`, `PermanentBlock`, ...); here a
/// single [`QueuedPacket::set_verdict`] takes the [`Verdict`] and the
/// implementation maps it to the mark. `RerouteToNameserver`, `RerouteToTunnel`
/// and `RerouteToSplitTun` are the matching `Verdict` variants.
pub trait QueuedPacket: Send + Sync {
    fn info(&self) -> &PacketInfo;

    fn connection_id(&self) -> String {
        self.info().connection_id()
    }

    fn is_outbound(&self) -> bool {
        !self.info().inbound
    }

    /// Info-only packets carry attribution data but no real packet.
    fn info_only(&self) -> bool {
        false
    }

    /// The packet source announces an info-only packet for the connection.
    /// Only used for packet reordering in Go, which the handler does not do.
    fn expect_info(&self) -> bool {
        false
    }

    /// Whether a verdict was already set, also by a timeout.
    fn verdict_set(&self) -> bool;

    fn fast_tracked_by_integration(&self) -> bool {
        false
    }

    /// The transport payload (Go `LoadPacketData` + `Payload`); `None` if the
    /// packet data could not be loaded.
    fn payload(&self) -> Option<&[u8]>;

    /// The complete IP packet, for ICMP listeners.
    fn raw(&self) -> &[u8];

    /// The ICMP/ICMPv6 type of the packet.
    fn icmp_type(&self) -> IcmpType;

    /// Sets the verdict. Only the first call can succeed.
    fn set_verdict(&self, verdict: Verdict) -> Result<(), VerdictError>;
}

/// A packet received from an nfqueue.
pub struct NfqPacket {
    info: PacketInfo,
    facts: PacketFacts,
    payload_offset: Option<usize>,
    raw: Vec<u8>,
    verdict: Mutex<Option<VerdictHandle>>,
    set: AtomicBool,
}

impl NfqPacket {
    pub fn new(packet: RawPacket) -> NfqPacket {
        let mut info = PacketInfo::from(&packet.info);
        info.inbound = packet.inbound;
        let facts = PacketFacts {
            inbound: packet.inbound,
            protocol: packet.info.ip_protocol(),
            icmpv6: packet.info.protocol == PROTO_ICMPV6,
            dst: packet.info.dst,
        };
        NfqPacket {
            info,
            facts,
            payload_offset: packet.info.payload_offset,
            raw: packet.payload,
            verdict: Mutex::new(Some(packet.verdict)),
            set: AtomicBool::new(false),
        }
    }
}

impl QueuedPacket for NfqPacket {
    fn info(&self) -> &PacketInfo {
        &self.info
    }

    fn verdict_set(&self) -> bool {
        self.set.load(Ordering::SeqCst)
    }

    fn payload(&self) -> Option<&[u8]> {
        match self.payload_offset {
            Some(offset) => self.raw.get(offset..),
            None => Some(&[]),
        }
    }

    fn raw(&self) -> &[u8] {
        &self.raw
    }

    fn icmp_type(&self) -> IcmpType {
        // Packets were parsed before, so loading cannot fail.
        icmp_type(&self.raw).map_or(IcmpType::NoLayer, IcmpType::Type)
    }

    fn set_verdict(&self, verdict: Verdict) -> Result<(), VerdictError> {
        let mut guard = self.verdict.lock().unwrap_or_else(|e| e.into_inner());
        let handle = guard.take().ok_or(VerdictError::AlreadySet)?;
        self.set.store(true, Ordering::SeqCst);
        handle.set(verdict.mark(&self.facts));
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use pm_interception::packet::parse_layer3;
    use pm_interception::Mark;

    fn udp_packet(dst: [u8; 4]) -> Vec<u8> {
        let mut p = vec![
            0x45, 0, 0, 0, 0, 0, 0, 0, 64, 17, 0, 0, 10, 0, 0, 1, dst[0], dst[1], dst[2], dst[3],
        ];
        p.extend_from_slice(&[0x13, 0x88, 0, 53, 0, 12, 0, 0, 1, 2, 3, 4]);
        p
    }

    fn nfq(
        raw: Vec<u8>,
        inbound: bool,
    ) -> (NfqPacket, tokio::sync::mpsc::UnboundedReceiver<(u32, Mark)>) {
        let (verdict, rx) = VerdictHandle::detached(7);
        let info = parse_layer3(&raw).unwrap();
        let packet = RawPacket {
            queue: 0,
            id: 7,
            inbound,
            info,
            payload: raw,
            verdict,
        };
        (NfqPacket::new(packet), rx)
    }

    #[test]
    fn exposes_info_and_payload() {
        let (pkt, _rx) = nfq(udp_packet([1, 1, 1, 1]), false);
        assert_eq!(pkt.info().dst_port, 53);
        assert!(pkt.is_outbound());
        assert_eq!(pkt.payload(), Some(&[1u8, 2, 3, 4][..]));
        assert_eq!(pkt.raw().len(), 32);
        assert_eq!(pkt.icmp_type(), IcmpType::NoLayer);
    }

    #[test]
    fn verdict_maps_to_marks_once() {
        let (pkt, mut rx) = nfq(udp_packet([1, 1, 1, 1]), false);
        assert!(!pkt.verdict_set());
        pkt.set_verdict(Verdict::PermanentAccept).unwrap();
        assert!(pkt.verdict_set());
        assert_eq!(rx.try_recv().unwrap(), (7, Mark::AcceptAlways));
        assert_eq!(
            pkt.set_verdict(Verdict::Drop),
            Err(VerdictError::AlreadySet)
        );
    }

    #[test]
    fn loopback_permanent_accept_is_not_sticky() {
        let (pkt, mut rx) = nfq(udp_packet([127, 0, 0, 1]), false);
        pkt.set_verdict(Verdict::PermanentAccept).unwrap();
        assert_eq!(rx.try_recv().unwrap(), (7, Mark::Accept));
    }

    #[test]
    fn direction_comes_from_the_queue() {
        let (pkt, _rx) = nfq(udp_packet([1, 1, 1, 1]), true);
        assert!(!pkt.is_outbound());
        assert!(pkt.info().inbound);
    }
}
