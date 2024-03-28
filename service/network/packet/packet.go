package packet

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/google/gopacket"
)

// Base is a base structure for satisfying the Packet interface.
type Base struct {
	ctx        context.Context
	info       Info
	connID     string
	layers     gopacket.Packet
	layer3Data []byte
	layer5Data []byte
}

// FastTrackedByIntegration returns whether the packet has been fast-track
// accepted by the OS integration.
func (pkt *Base) FastTrackedByIntegration() bool {
	return false
}

// InfoOnly returns whether the packet is informational only and does not
// represent an actual packet.
func (pkt *Base) InfoOnly() bool {
	return false
}

// ExpectInfo returns whether the next packet is expected to be informational only.
func (pkt *Base) ExpectInfo() bool {
	return false
}

// SetCtx sets the packet context.
func (pkt *Base) SetCtx(ctx context.Context) {
	pkt.ctx = ctx
}

// Ctx returns the packet context.
func (pkt *Base) Ctx() context.Context {
	return pkt.ctx
}

// Info returns the packet Info.
func (pkt *Base) Info() *Info {
	return &pkt.info
}

// SetPacketInfo sets a new packet Info. This must only used when initializing the packet structure.
func (pkt *Base) SetPacketInfo(packetInfo Info) {
	pkt.info = packetInfo
}

// SetInbound sets a the packet direction to inbound. This must only used when initializing the packet structure.
func (pkt *Base) SetInbound() {
	pkt.info.Inbound = true
}

// SetOutbound sets a the packet direction to outbound. This must only used when initializing the packet structure.
func (pkt *Base) SetOutbound() {
	pkt.info.Inbound = false
}

// IsInbound checks if the packet is inbound.
func (pkt *Base) IsInbound() bool {
	return pkt.info.Inbound
}

// IsOutbound checks if the packet is outbound.
func (pkt *Base) IsOutbound() bool {
	return !pkt.info.Inbound
}

// HasPorts checks if the packet has a protocol that uses ports.
func (pkt *Base) HasPorts() bool {
	switch pkt.info.Protocol {
	case TCP:
		return true
	case UDP, UDPLite:
		return true
	case ICMP, ICMPv6, IGMP, RAW, AnyHostInternalProtocol61:
		fallthrough
	default:
		return false
	}
}

// LoadPacketData loads packet data from the integration, if not yet done.
func (pkt *Base) LoadPacketData() error {
	return ErrFailedToLoadPayload
}

// Layers returns the parsed layer data.
func (pkt *Base) Layers() gopacket.Packet {
	return pkt.layers
}

// Raw returns the raw Layer 3 Network Data.
func (pkt *Base) Raw() []byte {
	return pkt.layer3Data
}

// Payload returns the raw Layer 5 Network Data.
func (pkt *Base) Payload() []byte {
	return pkt.layer5Data
}

// GetConnectionID returns the link ID for this packet.
func (pkt *Base) GetConnectionID() string {
	if pkt.connID == "" {
		pkt.connID = pkt.info.CreateConnectionID()
	}
	return pkt.connID
}

// MatchesAddress checks if a the packet matches a given endpoint (remote or local) in protocol, network and port.
//
// Comparison matrix:
//
// ======  IN   OUT
//
// Local   Dst  Src
// Remote  Src  Dst
// .
func (pkt *Base) MatchesAddress(remote bool, protocol IPProtocol, network *net.IPNet, port uint16) bool {
	if pkt.info.Protocol != protocol {
		return false
	}
	if pkt.info.Inbound != remote {
		if !network.Contains(pkt.info.Src) {
			return false
		}
		if pkt.info.SrcPort != port {
			return false
		}
	} else {
		if !network.Contains(pkt.info.Dst) {
			return false
		}
		if pkt.info.DstPort != port {
			return false
		}
	}
	return true
}

// MatchesIP checks if a the packet matches a given endpoint (remote or local) IP.
//
// Comparison matrix:
//
// ======  IN   OUT
//
// Local   Dst  Src
// Remote  Src  Dst
// .
func (pkt *Base) MatchesIP(endpoint bool, network *net.IPNet) bool {
	if pkt.info.Inbound != endpoint {
		if network.Contains(pkt.info.Src) {
			return true
		}
	} else {
		if network.Contains(pkt.info.Dst) {
			return true
		}
	}
	return false
}

// FORMATTING

func (pkt *Base) String() string {
	return pkt.FmtPacket()
}

// FmtPacket returns the most important information about the packet as a string.
func (pkt *Base) FmtPacket() string {
	if pkt.info.Protocol == TCP || pkt.info.Protocol == UDP {
		if pkt.info.Inbound {
			return fmt.Sprintf("IN %s %s:%d <-> %s:%d", pkt.info.Protocol, pkt.info.Dst, pkt.info.DstPort, pkt.info.Src, pkt.info.SrcPort)
		}
		return fmt.Sprintf("OUT %s %s:%d <-> %s:%d", pkt.info.Protocol, pkt.info.Src, pkt.info.SrcPort, pkt.info.Dst, pkt.info.DstPort)
	}
	if pkt.info.Inbound {
		return fmt.Sprintf("IN %s %s <-> %s", pkt.info.Protocol, pkt.info.Dst, pkt.info.Src)
	}
	return fmt.Sprintf("OUT %s %s <-> %s", pkt.info.Protocol, pkt.info.Src, pkt.info.Dst)
}

// FmtProtocol returns the protocol as a string.
func (pkt *Base) FmtProtocol() string {
	return pkt.info.Protocol.String()
}

// FmtRemoteIP returns the remote IP address as a string.
func (pkt *Base) FmtRemoteIP() string {
	if pkt.info.Inbound {
		return pkt.info.Src.String()
	}
	return pkt.info.Dst.String()
}

// FmtRemotePort returns the remote port as a string.
func (pkt *Base) FmtRemotePort() string {
	if pkt.info.SrcPort != 0 {
		if pkt.info.Inbound {
			return strconv.FormatUint(uint64(pkt.info.SrcPort), 10)
		}
		return strconv.FormatUint(uint64(pkt.info.DstPort), 10)
	}
	return "-"
}

// FmtRemoteAddress returns the full remote address (protocol, IP, port) as a string.
func (pkt *Base) FmtRemoteAddress() string {
	return fmt.Sprintf("%s:%s:%s", pkt.info.Protocol.String(), pkt.FmtRemoteIP(), pkt.FmtRemotePort())
}

// Packet is an interface to a network packet to provide object behavior the same across all systems.
type Packet interface {
	// Verdicts.
	Accept() error
	Block() error
	Drop() error
	PermanentAccept() error
	PermanentBlock() error
	PermanentDrop() error
	RerouteToNameserver() error
	RerouteToTunnel() error
	FastTrackedByIntegration() bool
	InfoOnly() bool
	ExpectInfo() bool

	// Info.
	SetCtx(ctx context.Context)
	Ctx() context.Context
	Info() *Info
	SetPacketInfo(info Info)
	IsInbound() bool
	IsOutbound() bool
	SetInbound()
	SetOutbound()
	HasPorts() bool
	GetConnectionID() string

	// Payload.
	LoadPacketData() error
	Layers() gopacket.Packet
	Raw() []byte
	Payload() []byte

	// Matching.
	MatchesAddress(remote bool, protocol IPProtocol, network *net.IPNet, port uint16) bool
	MatchesIP(endpoint bool, network *net.IPNet) bool

	// Formatting.
	String() string
	FmtPacket() string
	FmtProtocol() string
	FmtRemoteIP() string
	FmtRemotePort() string
	FmtRemoteAddress() string
}
