package packet

import (
	"fmt"
	"net"
	"time"
)

// Info holds IP and TCP/UDP header information.
type Info struct {
	Inbound  bool
	InTunnel bool

	Version          IPVersion
	Protocol         IPProtocol
	SrcPort, DstPort uint16
	Src, Dst         net.IP

	PID    int
	SeenAt time.Time
}

// LocalIP returns the local IP of the packet.
func (pi *Info) LocalIP() net.IP {
	if pi.Inbound {
		return pi.Dst
	}
	return pi.Src
}

// RemoteIP returns the remote IP of the packet.
func (pi *Info) RemoteIP() net.IP {
	if pi.Inbound {
		return pi.Src
	}
	return pi.Dst
}

// LocalPort returns the local port of the packet.
func (pi *Info) LocalPort() uint16 {
	if pi.Inbound {
		return pi.DstPort
	}
	return pi.SrcPort
}

// RemotePort returns the remote port of the packet.
func (pi *Info) RemotePort() uint16 {
	if pi.Inbound {
		return pi.SrcPort
	}
	return pi.DstPort
}

// CreateConnectionID creates a connection ID.
// In most circumstances, this method should not be used directly, but
// packet.GetConnectionID() should be called instead.
func (pi *Info) CreateConnectionID() string {
	return CreateConnectionID(pi.Protocol, pi.Src, pi.SrcPort, pi.Dst, pi.DstPort, pi.Inbound)
}

// CreateConnectionID creates a connection ID.
func CreateConnectionID(protocol IPProtocol, src net.IP, srcPort uint16, dst net.IP, dstPort uint16, inbound bool) string {
	// TODO: make this ID not depend on the packet direction for better support for forwarded packets.
	if protocol == TCP || protocol == UDP {
		if inbound {
			return fmt.Sprintf("%d-%s-%d-%s-%d", protocol, dst, dstPort, src, srcPort)
		}
		return fmt.Sprintf("%d-%s-%d-%s-%d", protocol, src, srcPort, dst, dstPort)
	}

	if inbound {
		return fmt.Sprintf("%d-%s-%s", protocol, dst, src)
	}
	return fmt.Sprintf("%d-%s-%s", protocol, src, dst)
}
