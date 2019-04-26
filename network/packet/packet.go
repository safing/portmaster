// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package packet

import (
	"errors"
	"fmt"
	"net"
)

type (
	IPVersion  uint8
	IPProtocol uint8
	Verdict    uint8
	Endpoint   bool
)

const (
	IPv4 = IPVersion(4)
	IPv6 = IPVersion(6)

	InBound  = true
	OutBound = false

	Local  = true
	Remote = false

	// convenience
	IGMP   = IPProtocol(2)
	RAW    = IPProtocol(255)
	TCP    = IPProtocol(6)
	UDP    = IPProtocol(17)
	ICMP   = IPProtocol(1)
	ICMPv6 = IPProtocol(58)
)

const (
	DROP Verdict = iota
	BLOCK
	ACCEPT
	STOLEN
	QUEUE
	REPEAT
	STOP
)

var (
	ErrFailedToLoadPayload = errors.New("could not load packet payload")
)

// Returns the byte size of the ip, IPv4 = 4 bytes, IPv6 = 16
func (v IPVersion) ByteSize() int {
	switch v {
	case IPv4:
		return 4
	case IPv6:
		return 16
	}
	return 0
}

func (v IPVersion) String() string {
	switch v {
	case IPv4:
		return "IPv4"
	case IPv6:
		return "IPv6"
	}
	return fmt.Sprintf("<unknown ip version, %d>", uint8(v))
}

func (p IPProtocol) String() string {
	switch p {
	case RAW:
		return "RAW"
	case TCP:
		return "TCP"
	case UDP:
		return "UDP"
	case ICMP:
		return "ICMP"
	case ICMPv6:
		return "ICMPv6"
	case IGMP:
		return "IGMP"
	}
	return fmt.Sprintf("<unknown protocol, %d>", uint8(p))
}

func (v Verdict) String() string {
	switch v {
	case DROP:
		return "DROP"
	case ACCEPT:
		return "ACCEPT"
	}
	return fmt.Sprintf("<unsupported verdict, %d>", uint8(v))
}

// PacketInfo holds IP and TCP/UDP header information
type PacketInfo struct {
	Direction bool
	InTunnel  bool

	Version          IPVersion
	Src, Dst         net.IP
	Protocol         IPProtocol
	SrcPort, DstPort uint16
}

type PacketBase struct {
	info    PacketInfo
	linkID  string
	Payload []byte
}

func (pkt *PacketBase) Info() *PacketInfo {
	return &pkt.info
}

func (pkt *PacketBase) SetPacketInfo(packetInfo PacketInfo) {
	pkt.info = packetInfo
}

func (pkt *PacketBase) SetInbound() {
	pkt.info.Direction = true
}

func (pkt *PacketBase) SetOutbound() {
	pkt.info.Direction = false
}

func (pkt *PacketBase) IsInbound() bool {
	return pkt.info.Direction
}

func (pkt *PacketBase) IsOutbound() bool {
	return !pkt.info.Direction
}

func (pkt *PacketBase) HasPorts() bool {
	switch pkt.info.Protocol {
	case TCP:
		return true
	case UDP:
		return true
	}
	return false
}

func (pkt *PacketBase) GetPayload() ([]byte, error) {
	return pkt.Payload, ErrFailedToLoadPayload
}

func (pkt *PacketBase) GetLinkID() string {
	if pkt.linkID == "" {
		pkt.createLinkID()
	}
	return pkt.linkID
}

func (pkt *PacketBase) createLinkID() {
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

// Matches checks if a the packet matches a given endpoint (remote or local) in protocol, network and port.
//
// Comparison matrix:
//         IN   OUT
// Local   Dst  Src
// Remote  Src  Dst
//
func (pkt *PacketBase) MatchesAddress(remote bool, protocol IPProtocol, network *net.IPNet, port uint16) bool {
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

func (pkt *PacketBase) MatchesIP(endpoint bool, network *net.IPNet) bool {
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

func (pkt *PacketBase) String() string {
	return pkt.FmtPacket()
}

// FmtPacket returns the most important information about the packet as a string
func (pkt *PacketBase) FmtPacket() string {
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
func (pkt *PacketBase) FmtProtocol() string {
	return pkt.info.Protocol.String()
}

// FmtRemoteIP returns the remote IP address as a string
func (pkt *PacketBase) FmtRemoteIP() string {
	if pkt.info.Direction {
		return pkt.info.Src.String()
	}
	return pkt.info.Dst.String()
}

// FmtRemotePort returns the remote port as a string
func (pkt *PacketBase) FmtRemotePort() string {
	if pkt.info.SrcPort != 0 {
		if pkt.info.Direction {
			return fmt.Sprintf("%d", pkt.info.SrcPort)
		}
		return fmt.Sprintf("%d", pkt.info.DstPort)
	}
	return "-"
}

// FmtRemoteAddress returns the full remote address (protocol, IP, port) as a string
func (pkt *PacketBase) FmtRemoteAddress() string {
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
	Info() *PacketInfo
	SetPacketInfo(PacketInfo)
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
