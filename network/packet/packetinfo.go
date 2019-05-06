// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package packet

import (
	"net"
)

// Info holds IP and TCP/UDP header information
type Info struct {
	Direction bool
	InTunnel  bool

	Version          IPVersion
	Src, Dst         net.IP
	Protocol         IPProtocol
	SrcPort, DstPort uint16
}

// LocalIP returns the local IP of the packet.
func (pi *Info) LocalIP() net.IP {
	if pi.Direction {
		return pi.Dst
	}
	return pi.Src
}

// RemoteIP returns the remote IP of the packet.
func (pi *Info) RemoteIP() net.IP {
	if pi.Direction {
		return pi.Src
	}
	return pi.Dst
}

// LocalPort returns the local port of the packet.
func (pi *Info) LocalPort() uint16 {
	if pi.Direction {
		return pi.DstPort
	}
	return pi.SrcPort
}

// RemotePort returns the remote port of the packet.
func (pi *Info) RemotePort() uint16 {
	if pi.Direction {
		return pi.SrcPort
	}
	return pi.DstPort
}
