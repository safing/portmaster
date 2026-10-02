package network

import (
	"testing"
	"time"

	"github.com/safing/portmaster/service/network/packet"
)

func TestUnusablePorts(t *testing.T) {
	t.Parallel()

	const (
		udp  = uint8(packet.UDP)
		tcp  = uint8(packet.TCP)
		port = uint16(12345)
	)

	if portIsKnownUnusable(udp, port) {
		t.Fatal("port must not be unusable before it is marked")
	}

	MarkPortUnusable(udp, port)
	if !portIsKnownUnusable(udp, port) {
		t.Error("port must be unusable after it is marked")
	}
	if portIsKnownUnusable(tcp, port) {
		t.Error("marking a UDP port must not affect the TCP port")
	}
	if portIsKnownUnusable(udp, port+1) {
		t.Error("marking a port must not affect its neighbour")
	}

	// Expire the entry.
	unusablePortsLock.Lock()
	unusablePorts[unusablePortKey(udp, port)] = time.Now().Add(-time.Second)
	unusablePortsLock.Unlock()

	if portIsKnownUnusable(udp, port) {
		t.Error("port must be usable again after the entry expired")
	}
	unusablePortsLock.Lock()
	_, stillThere := unusablePorts[unusablePortKey(udp, port)]
	unusablePortsLock.Unlock()
	if stillThere {
		t.Error("expired entry must be removed")
	}
}
