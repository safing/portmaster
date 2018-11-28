// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package packet

import (
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

type IPHeader struct {
	Version IPVersion

	Tos, TTL uint8
	Protocol IPProtocol
	Src, Dst net.IP
}

type TCPUDPHeader struct {
	SrcPort, DstPort uint16
	Checksum         uint16 //not implemented
}

type PacketBase struct {
	linkID    string
	Direction bool
	InTunnel  bool
	Payload   []byte
	*IPHeader
	*TCPUDPHeader
}

func (pkt *PacketBase) GetIPHeader() *IPHeader {
	return pkt.IPHeader
}

func (pkt *PacketBase) GetTCPUDPHeader() *TCPUDPHeader {
	return pkt.TCPUDPHeader
}

func (pkt *PacketBase) GetPayload() []byte {
	return pkt.Payload
}

func (pkt *PacketBase) SetInbound() {
	pkt.Direction = true
}

func (pkt *PacketBase) SetOutbound() {
	pkt.Direction = false
}

func (pkt *PacketBase) IsInbound() bool {
	return pkt.Direction
}

func (pkt *PacketBase) IsOutbound() bool {
	return !pkt.Direction
}

func (pkt *PacketBase) IPVersion() IPVersion {
	return pkt.Version
}

func (pkt *PacketBase) GetLinkID() string {
	if pkt.linkID == "" {
		pkt.createLinkID()
	}
	return pkt.linkID
}

func (pkt *PacketBase) createLinkID() {
	if pkt.IPHeader.Protocol == TCP || pkt.IPHeader.Protocol == UDP {
		if pkt.Direction {
			pkt.linkID = fmt.Sprintf("%d-%s-%d-%s-%d", pkt.Protocol, pkt.Dst, pkt.DstPort, pkt.Src, pkt.SrcPort)
		} else {
			pkt.linkID = fmt.Sprintf("%d-%s-%d-%s-%d", pkt.Protocol, pkt.Src, pkt.SrcPort, pkt.Dst, pkt.DstPort)
		}
	} else {
		if pkt.Direction {
			pkt.linkID = fmt.Sprintf("%d-%s-%s", pkt.Protocol, pkt.Dst, pkt.Src)
		} else {
			pkt.linkID = fmt.Sprintf("%d-%s-%s", pkt.Protocol, pkt.Src, pkt.Dst)
		}
	}
}

// Matches checks if a the packet matches a given endpoint (remote or local) in protocol, network and port.
//
//         IN   OUT
// Local   Dst  Src
// Remote  Src  Dst
//
func (pkt *PacketBase) MatchesAddress(endpoint bool, protocol IPProtocol, network *net.IPNet, port uint16) bool {
	if pkt.Protocol != protocol {
		return false
	}
	if pkt.Direction != endpoint {
		if !network.Contains(pkt.Src) {
			return false
		}
		if port != 0 && pkt.TCPUDPHeader != nil {
			if pkt.SrcPort != port {
				return false
			}
		}
	} else {
		if !network.Contains(pkt.Dst) {
			return false
		}
		if port != 0 && pkt.TCPUDPHeader != nil {
			if pkt.DstPort != port {
				return false
			}
		}
	}
	return true
}

func (pkt *PacketBase) MatchesIP(endpoint bool, network *net.IPNet) bool {
	if pkt.Direction != endpoint {
		if network.Contains(pkt.Src) {
			return true
		}
	} else {
		if network.Contains(pkt.Dst) {
			return true
		}
	}
	return false
}

// func (pkt *PacketBase) Accept() error {
//   return nil
// }
//
// func (pkt *PacketBase) Drop() error {
//   return nil
// }
//
// func (pkt *PacketBase) Block() error {
//   return nil
// }
//
// func (pkt *PacketBase) Verdict(verdict Verdict) error {
//   return nil
// }

// FORMATTING

func (pkt *PacketBase) String() string {
	return pkt.FmtPacket()
}

// FmtPacket returns the most important information about the packet as a string
func (pkt *PacketBase) FmtPacket() string {
	if pkt.IPHeader.Protocol == TCP || pkt.IPHeader.Protocol == UDP {
		if pkt.Direction {
			return fmt.Sprintf("IN %s %s:%d <-> %s:%d", pkt.Protocol, pkt.Dst, pkt.DstPort, pkt.Src, pkt.SrcPort)
		}
		return fmt.Sprintf("OUT %s %s:%d <-> %s:%d", pkt.Protocol, pkt.Src, pkt.SrcPort, pkt.Dst, pkt.DstPort)
	}
	if pkt.Direction {
		return fmt.Sprintf("IN %s %s <-> %s", pkt.Protocol, pkt.Dst, pkt.Src)
	}
	return fmt.Sprintf("OUT %s %s <-> %s", pkt.Protocol, pkt.Src, pkt.Dst)
}

// FmtProtocol returns the protocol as a string
func (pkt *PacketBase) FmtProtocol() string {
	return pkt.IPHeader.Protocol.String()
}

// FmtRemoteIP returns the remote IP address as a string
func (pkt *PacketBase) FmtRemoteIP() string {
	if pkt.Direction {
		return pkt.IPHeader.Src.String()
	}
	return pkt.IPHeader.Dst.String()
}

// FmtRemotePort returns the remote port as a string
func (pkt *PacketBase) FmtRemotePort() string {
	if pkt.TCPUDPHeader != nil {
		if pkt.Direction {
			return fmt.Sprintf("%d", pkt.TCPUDPHeader.SrcPort)
		}
		return fmt.Sprintf("%d", pkt.TCPUDPHeader.DstPort)
	}
	return "-"
}

// FmtRemoteAddress returns the full remote address (protocol, IP, port) as a string
func (pkt *PacketBase) FmtRemoteAddress() string {
	return fmt.Sprintf("%s:%s:%s", pkt.IPHeader.Protocol.String(), pkt.FmtRemoteIP(), pkt.FmtRemotePort())
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
	GetIPHeader() *IPHeader
	GetTCPUDPHeader() *TCPUDPHeader
	GetPayload() []byte
	IsInbound() bool
	IsOutbound() bool
	SetInbound()
	SetOutbound()
	GetLinkID() string
	IPVersion() IPVersion

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
