//go:build !windows

package netenv

import "net"

func newICMPListener(_ string) (net.PacketConn, error) { //nolint:unused,deadcode // TODO: clean with Windows code later.
	return net.ListenPacket("ip4:icmp", "0.0.0.0")
}
