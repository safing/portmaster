// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package packet

import (
	"context"
	"fmt"
	"net"
)

// Base is a base structure for satisfying the Packet interface.
type Base struct {
	ctx     context.Context
	info    Info
	linkID  string
	Payload []byte
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
	pkt.info.Direction = true
}

// SetOutbound sets a the packet direction to outbound. This must only used when initializing the packet structure.
func (pkt *Base) SetOutbound() {
	pkt.info.Direction = false
}

// IsInbound checks if the packet is inbound.
func (pkt *Base) IsInbound() bool {
	return pkt.info.Direction
}

// IsOutbound checks if the packet is outbound.
func (pkt *Base) IsOutbound() bool {
	return !pkt.info.Direction
}

// HasPorts checks if the packet has a protocol that uses ports.
func (pkt *Base) HasPorts() bool {
	switch pkt.info.Protocol {
	case TCP:
		return true
	case UDP:
		return true
	}
	return false
}

// GetPayload returns the packet payload. In some cases, this will fetch the payload from the os integration system.
func (pkt *Base) GetPayload() ([]byte, error) {
	return pkt.Payload, ErrFailedToLoadPayload
}

// GetLinkID returns the link ID for this packet.
func (pkt *Base) GetLinkID() string {
	if pkt.linkID == "" {
		pkt.createLinkID()
	}
	return pkt.linkID
}

func (pkt *Base) createLinkID() {
	if pkt.info.Protocol == TCP || pkt.info.Protocol == UDP {
		if pkt.info.Direction {
			pkt.linkID = fmt.Sprintf("%d-%s-%d-%s-%d", pkt.info.Protocol, pkt.info.Dst, pkt.info.DstPort, pkt.info.Src, pkt.info.SrcPort)
		} else {
			pkt.linkID = fmt.Sprintf("%d-%s-%d-%s-%d", pkt.info.Protocol, pkt.info.Src, pkt.info.SrcPort, pkt.info.Dst, pkt.info.DstPort)
		}
	} else {
		if pkt.info.Direction {
			pkt.linkID = fmt.Sprintf("%d-%s-%s", pkt.info.Protocol, pkt.info.Dst, pkt.info.Src)
		} else {
			pkt.linkID = fmt.Sprintf("%d-%s-%s", pkt.info.Protocol, pkt.info.Src, pkt.info.Dst)
		}
	}
}

// MatchesAddress checks if a the packet matches a given endpoint (remote or local) in protocol, network and port.
//
// Comparison matrix:
//         IN   OUT
// Local   Dst  Src
// Remote  Src  Dst
//
func (pkt *Base) MatchesAddress(remote bool, protocol IPProtocol, network *net.IPNet, port uint16) bool {
	if pkt.info.Protocol != protocol {
		return false
	}
	if pkt.info.Direction != remote {
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
//         IN   OUT
// Local   Dst  Src
// Remote  Src  Dst
//
func (pkt *Base) MatchesIP(endpoint bool, network *net.IPNet) bool {
	if pkt.info.Direction != endpoint {
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

// FmtPacket returns the most important information about the packet as a string
func (pkt *Base) FmtPacket() string {
	if pkt.info.Protocol == TCP || pkt.info.Protocol == UDP {
		if pkt.info.Direction {
			return fmt.Sprintf("IN %s %s:%d <-> %s:%d", pkt.info.Protocol, pkt.info.Dst, pkt.info.DstPort, pkt.info.Src, pkt.info.SrcPort)
		}
		return fmt.Sprintf("OUT %s %s:%d <-> %s:%d", pkt.info.Protocol, pkt.info.Src, pkt.info.SrcPort, pkt.info.Dst, pkt.info.DstPort)
	}
	if pkt.info.Direction {
		return fmt.Sprintf("IN %s %s <-> %s", pkt.info.Protocol, pkt.info.Dst, pkt.info.Src)
	}
	return fmt.Sprintf("OUT %s %s <-> %s", pkt.info.Protocol, pkt.info.Src, pkt.info.Dst)
}

// FmtProtocol returns the protocol as a string
func (pkt *Base) FmtProtocol() string {
	return pkt.info.Protocol.String()
}

// FmtRemoteIP returns the remote IP address as a string
func (pkt *Base) FmtRemoteIP() string {
	if pkt.info.Direction {
		return pkt.info.Src.String()
	}
	return pkt.info.Dst.String()
}

// FmtRemotePort returns the remote port as a string
func (pkt *Base) FmtRemotePort() string {
	if pkt.info.SrcPort != 0 {
		if pkt.info.Direction {
			return fmt.Sprintf("%d", pkt.info.SrcPort)
		}
		return fmt.Sprintf("%d", pkt.info.DstPort)
	}
	return "-"
}

// FmtRemoteAddress returns the full remote address (protocol, IP, port) as a string
func (pkt *Base) FmtRemoteAddress() string {
	return fmt.Sprintf("%s:%s:%s", pkt.info.Protocol.String(), pkt.FmtRemoteIP(), pkt.FmtRemotePort())
}

// Packet is an interface to a network packet to provide object behaviour the same across all systems
type Packet interface {
	// VERDICTS
	Accept() error
	Block() error
	Drop() error
	PermanentAccept() error
	PermanentBlock() error
	PermanentDrop() error
	RerouteToNameserver() error
	RerouteToTunnel() error

	// INFO
	SetCtx(context.Context)
	Ctx() context.Context
	Info() *Info
	SetPacketInfo(Info)
	IsInbound() bool
	IsOutbound() bool
	SetInbound()
	SetOutbound()
	HasPorts() bool
	GetPayload() ([]byte, error)
	GetLinkID() string

	// MATCHING
	MatchesAddress(bool, IPProtocol, *net.IPNet, uint16) bool
	MatchesIP(bool, *net.IPNet) bool

	// FORMATTING
	String() string
	FmtPacket() string
	FmtProtocol() string
	FmtRemoteIP() string
	FmtRemotePort() string
	FmtRemoteAddress() string
}
