package firewall

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/resolver"
)

var (
	preAuthenticatedPorts     = make(map[string]struct{})
	preAuthenticatedPortsLock sync.Mutex
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
	preAuthdPort := GetPermittedPort(packet.UDP)
	if preAuthdPort == 0 {
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", preAuthdPort))
	if err != nil {
		return nil
	}

	return addr
}

// PermittedTCPAddr returns an already permitted local tcp address for reliable connectivity.
// Returns nil in case of error.
func PermittedTCPAddr() *net.TCPAddr {
	preAuthdPort := GetPermittedPort(packet.TCP)
	if preAuthdPort == 0 {
		return nil
	}

	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", preAuthdPort))
	if err != nil {
		return nil
	}

	return addr
}

// GetPermittedPort returns a local port number that is already permitted for communication.
// This bypasses the process attribution step to guarantee connectivity.
// Communication on the returned port is attributed to the Portmaster.
// Every pre-authenticated port is only valid once.
// If no unused local port number can be found, it will return 0, which is
// expected to trigger automatic port selection by the underlying OS.
func GetPermittedPort(protocol packet.IPProtocol) uint16 {
	port, ok := network.GetUnusedLocalPort(uint8(protocol))
	if !ok {
		return 0
	}

	preAuthenticatedPortsLock.Lock()
	defer preAuthenticatedPortsLock.Unlock()

	// Save generated port.
	key := generateLocalPreAuthKey(uint8(protocol), port)
	preAuthenticatedPorts[key] = struct{}{}

	return port
}

// localPortIsPreAuthenticated checks if the given protocol and port are
// pre-authenticated and should be attributed to the Portmaster itself.
func localPortIsPreAuthenticated(protocol uint8, port uint16) bool {
	preAuthenticatedPortsLock.Lock()
	defer preAuthenticatedPortsLock.Unlock()

	// Check if the given protocol and port are pre-authenticated.
	key := generateLocalPreAuthKey(protocol, port)
	_, ok := preAuthenticatedPorts[key]
	if ok {
		// Immediately remove pre authenticated port.
		delete(preAuthenticatedPorts, key)
	}

	return ok
}

// generateLocalPreAuthKey creates a map key for the pre-authenticated ports.
func generateLocalPreAuthKey(protocol uint8, port uint16) string {
	return strconv.Itoa(int(protocol)) + ":" + strconv.Itoa(int(port))
}
