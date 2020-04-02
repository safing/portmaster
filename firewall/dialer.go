package firewall

import (
	"fmt"
	"net"

	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/resolver"
)

func init() {
	resolver.SetLocalAddrFactory(PermittedAddr)
	netenv.SetLocalAddrFactory(PermittedAddr)
}

// PermittedAddr returns an already permitted local address for the given network for reliable connectivity.
// Returns nil in case of error.
func PermittedAddr(network string) net.Addr {
	switch network {
	case "udp":
		return PermittedUDPAddr()
	case "tcp":
		return PermittedTCPAddr()
	}
	return nil
}

// PermittedUDPAddr returns an already permitted local udp address for reliable connectivity.
// Returns nil in case of error.
func PermittedUDPAddr() *net.UDPAddr {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", GetPermittedPort()))
	if err != nil {
		return nil
	}
	return addr
}

// PermittedTCPAddr returns an already permitted local tcp address for reliable connectivity.
// Returns nil in case of error.
func PermittedTCPAddr() *net.TCPAddr {
	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", GetPermittedPort()))
	if err != nil {
		return nil
	}
	return addr
}
