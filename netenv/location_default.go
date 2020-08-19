//+build !windows

package netenv

import "net"

func newICMPListener(_ string) (net.PacketConn, error) {
	return net.ListenPacket("ip4:icmp", "0.0.0.0")
}
