package packet

import (
	"net"
)

// Info holds IP and TCP/UDP header information.
type Info struct {
	Inbound  bool
	InTunnel bool

	Version          IPVersion
	Protocol         IPProtocol
	SrcPort, DstPort uint16
	Src, Dst         net.IP
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
