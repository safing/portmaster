package firewall

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/safing/portmaster/base/log"
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
	resolver.SetLocalPortHooks(resolver.LocalPortHooks{
		Authorize: authorizeLocalPort,
		Release:   releaseLocalPort,
	})
	netenv.SetLocalAddrFactory(PermittedAddr)
	netenv.SetLocalPortReleaser(releaseLocalPort)
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

	registerPreAuthenticatedPort(uint8(protocol), port)
	return port
}

// authorizeLocalPort pre-authenticates a local port that Portmaster itself
// has already bound, so that the first packet is attributed to Portmaster.
func authorizeLocalPort(netw string, port uint16) {
	protocol, ok := protocolFromNetwork(netw)
	if !ok {
		log.Warningf("filter: cannot pre-authenticate port %d of unsupported network %q", port, netw)
		return
	}

	registerPreAuthenticatedPort(uint8(protocol), port)
}

// releaseLocalPort removes a pre-authentication that was not used, because
// the connection was never established. If unusable is set, the port could
// not be bound and is skipped by port selection for a while.
func releaseLocalPort(netw string, port uint16, unusable bool) {
	protocol, ok := protocolFromNetwork(netw)
	if !ok {
		log.Warningf("filter: cannot release pre-authenticated port %d of unsupported network %q", port, netw)
		return
	}

	unregisterPreAuthenticatedPort(uint8(protocol), port)
	if unusable {
		network.MarkPortUnusable(uint8(protocol), port)
	}
}

func protocolFromNetwork(netw string) (packet.IPProtocol, bool) {
	switch netw {
	case "udp":
		return packet.UDP, true
	case "tcp":
		return packet.TCP, true
	default:
		return 0, false
	}
}

func registerPreAuthenticatedPort(protocol uint8, port uint16) {
	preAuthenticatedPortsLock.Lock()
	defer preAuthenticatedPortsLock.Unlock()

	preAuthenticatedPorts[generateLocalPreAuthKey(protocol, port)] = struct{}{}
}

func unregisterPreAuthenticatedPort(protocol uint8, port uint16) {
	preAuthenticatedPortsLock.Lock()
	defer preAuthenticatedPortsLock.Unlock()

	delete(preAuthenticatedPorts, generateLocalPreAuthKey(protocol, port))
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
