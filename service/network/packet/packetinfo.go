package packet

import (
	"net"
	"net/netip"
	"strconv"
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
	// The output format is identical to the former fmt.Sprintf based
	// implementation ("%d-%s-%d-%s-%d" / "%d-%s-%s"), but is built in a
	// stack buffer to avoid per-packet allocations.
	var stack [96]byte
	buf := strconv.AppendUint(stack[:0], uint64(protocol), 10)

	// TODO: make this ID not depend on the packet direction for better support for forwarded packets.
	if inbound {
		src, dst = dst, src
		srcPort, dstPort = dstPort, srcPort
	}

	buf = append(buf, '-')
	buf = appendIP(buf, src)
	if protocol == TCP || protocol == UDP {
		buf = append(buf, '-')
		buf = strconv.AppendUint(buf, uint64(srcPort), 10)
	}
	buf = append(buf, '-')
	buf = appendIP(buf, dst)
	if protocol == TCP || protocol == UDP {
		buf = append(buf, '-')
		buf = strconv.AppendUint(buf, uint64(dstPort), 10)
	}

	return string(buf)
}

// appendIP appends the text form of ip, identical to ip.String().
func appendIP(buf []byte, ip net.IP) []byte {
	if len(ip) == net.IPv4len || len(ip) == net.IPv6len {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			// net.IP.String prints 4-in-6 addresses as dotted quad.
			return addr.Unmap().AppendTo(buf)
		}
	}
	// nil/empty and invalid lengths ("<nil>", "?hex").
	return append(buf, ip.String()...)
}
