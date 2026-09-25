package firewall

import (
	"testing"

	"github.com/safing/portmaster/service/network/packet"
)

func TestPreAuthenticatedPorts(t *testing.T) {
	t.Parallel()

	const (
		udp  = uint8(packet.UDP)
		tcp  = uint8(packet.TCP)
		port = uint16(40000)
	)

	if localPortIsPreAuthenticated(udp, port) {
		t.Fatal("port must not be pre-authenticated initially")
	}

	// A registered port is valid exactly once.
	registerPreAuthenticatedPort(udp, port)
	if !localPortIsPreAuthenticated(udp, port) {
		t.Error("registered port must be pre-authenticated")
	}
	if localPortIsPreAuthenticated(udp, port) {
		t.Error("pre-authentication must be consumed by the first check")
	}

	// A released port is not valid anymore.
	registerPreAuthenticatedPort(udp, port)
	unregisterPreAuthenticatedPort(udp, port)
	if localPortIsPreAuthenticated(udp, port) {
		t.Error("released port must not be pre-authenticated")
	}

	// Protocols are kept apart.
	registerPreAuthenticatedPort(udp, port)
	if localPortIsPreAuthenticated(tcp, port) {
		t.Error("UDP registration must not pre-authenticate the TCP port")
	}
	unregisterPreAuthenticatedPort(udp, port)

	// The hooks map network names to protocols.
	authorizeLocalPort("tcp", port)
	if !localPortIsPreAuthenticated(tcp, port) {
		t.Error("authorized tcp port must be pre-authenticated")
	}
	authorizeLocalPort("udp", port)
	releaseLocalPort("udp", port, false)
	if localPortIsPreAuthenticated(udp, port) {
		t.Error("released udp port must not be pre-authenticated")
	}

	// Unsupported networks are ignored without panicking.
	authorizeLocalPort("sctp", port)
	releaseLocalPort("sctp", port, true)
}
