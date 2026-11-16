//go:build linux

package nfq

import (
	"net"
	"testing"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network"
	pmpacket "github.com/safing/portmaster/service/network/packet"
)

func testConnection() *network.Connection {
	return &network.Connection{
		IPVersion: pmpacket.IPv4,
		LocalIP:   net.IPv4(10, 0, 0, 1),
		LocalPort: 40000,
		Entity: &intel.Entity{
			IP:       net.IPv4(10, 0, 0, 2),
			Protocol: 6,
			Port:     443,
		},
	}
}

// TestConntrackUninitialized checks that all operations report an error
// instead of using a nil (or closed) handle, and that teardown is idempotent.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestConntrackUninitialized(t *testing.T) {
	TeardownNFCT()
	TeardownNFCT()

	if DeleteUnmarkedConnections() == nil {
		t.Error("DeleteUnmarkedConnections: expected error on uninitialized handle")
	}
	if DeleteAllMarkedConnection() == nil {
		t.Error("DeleteAllMarkedConnection: expected error on uninitialized handle")
	}
	if DeleteMarkedConnection(testConnection()) == nil {
		t.Error("DeleteMarkedConnection: expected error on uninitialized handle")
	}
}
