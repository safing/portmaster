//go:build linux

package nfq

import (
	"errors"
	"net"
	"testing"

	ct "github.com/florianl/go-conntrack"

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

// TestDeleteMarkedConnectionIncomplete checks that a connection without
// remote entity or local address is rejected instead of dereferencing nil.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestDeleteMarkedConnectionIncomplete(t *testing.T) {
	h := &fakeHandle{}
	installFakeConntrack(t, h)

	noEntity := testConnection()
	noEntity.Entity = nil
	noEntityIP := testConnection()
	noEntityIP.Entity.IP = nil
	noLocalIP := testConnection()
	noLocalIP.LocalIP = nil

	for _, conn := range []*network.Connection{noEntity, noEntityIP, noLocalIP} {
		err := DeleteMarkedConnection(conn)
		if !errors.Is(err, errConnectionIncomplete) {
			t.Errorf("expected errConnectionIncomplete, got %v", err)
		}
	}
	if h.deletes != 0 {
		t.Errorf("no request must be sent for incomplete connections, got %d", h.deletes)
	}
}

// TestDeleteFlow checks that DeleteFlow deletes by the given original tuple
// and treats a missing entry as success.
//
//nolint:paralleltest // Modifies the package-level conntrack handle.
func TestDeleteFlow(t *testing.T) {
	localIP, remoteIP := net.IPv4(10, 0, 2, 15).To4(), net.IPv4(10, 0, 2, 3).To4()

	// No entry: ENOENT is not an error.
	h := &fakeHandle{}
	installFakeConntrack(t, h)
	deleted, err := DeleteFlow(17, localIP, 59823, remoteIP, 53)
	if err != nil || deleted {
		t.Fatalf("missing entry: expected no error and no delete, got %v, %v", deleted, err)
	}

	// Existing entry: deleted by the original tuple.
	h = &fakeHandle{entries: []ct.Con{testEntry()}}
	installFakeConntrack(t, h)
	deleted, err = DeleteFlow(17, localIP, 59823, remoteIP, 53)
	if err != nil || !deleted {
		t.Fatalf("existing entry: expected a delete, got %v, %v", deleted, err)
	}
	if len(h.deleted) != 1 {
		t.Fatalf("expected 1 delete, got %d", len(h.deleted))
	}
	origin := h.deleted[0].Origin
	switch {
	case !origin.Src.Equal(localIP) || !origin.Dst.Equal(remoteIP):
		t.Errorf("deleted %s -> %s, expected %s -> %s", origin.Src, origin.Dst, localIP, remoteIP)
	case *origin.Proto.Number != 17 || *origin.Proto.SrcPort != 59823 || *origin.Proto.DstPort != 53:
		t.Errorf("deleted protocol %d ports %d -> %d, expected 17 ports 59823 -> 53",
			*origin.Proto.Number, *origin.Proto.SrcPort, *origin.Proto.DstPort)
	}

	// Other errors are returned.
	h = &fakeHandle{deleteErr: errFakeOpenFailed}
	installFakeConntrack(t, h, &fakeHandle{deleteErr: errFakeOpenFailed})
	_, err = DeleteFlow(17, localIP, 59823, remoteIP, 53)
	if err == nil {
		t.Error("expected an error to be returned")
	}
}
