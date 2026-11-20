package network

import (
	"net"
	"testing"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/packet"
)

// newReuseTestConnection adds an incomplete UDP connection to the connection
// store. Incomplete connections are not written to the database.
func newReuseTestConnection(t *testing.T, localPort uint16) *Connection {
	t.Helper()

	localIP, remoteIP := net.IPv4(10, 0, 2, 15), net.IPv4(10, 0, 2, 3)
	conn := &Connection{
		ID:           packet.CreateConnectionID(packet.UDP, localIP, localPort, remoteIP, 53, false),
		Type:         IPConnection,
		IPProtocol:   packet.UDP,
		LocalIP:      localIP,
		LocalPort:    localPort,
		Entity:       &intel.Entity{IP: remoteIP, Port: 53},
		Verdict:      VerdictRerouteToNameserver,
		Started:      1,
		dataComplete: abool.New(),
	}
	conn.UpdateMeta() // As NewIncompleteConnection does.
	conns.add(conn)
	t.Cleanup(func() { conns.delete(conn) })
	return conn
}

func TestRemoveEndedConnection(t *testing.T) { //nolint:paralleltest // Modifies the global connection store.
	conn := newReuseTestConnection(t, 40001)

	// Active connections are kept.
	if RemoveEndedConnection(conn) {
		t.Fatal("removed a connection that has not ended")
	}
	if _, ok := GetConnection(conn.ID); !ok {
		t.Fatal("active connection is gone")
	}

	// A locked connection is kept, the dispatcher must not wait for it.
	conn.Lock()
	conn.Ended = 2
	conn.ended.Store(true)
	removed := RemoveEndedConnection(conn)
	conn.Unlock()
	if removed {
		t.Fatal("removed a locked connection")
	}

	// An ended connection is removed.
	if !conn.HasEnded() || !RemoveEndedConnection(conn) {
		t.Fatal("ended connection was not removed")
	}
	if _, ok := GetConnection(conn.ID); ok {
		t.Fatal("ended connection is still in the state")
	}
}

func TestEndConnectionForReusedTuple(t *testing.T) { //nolint:paralleltest // Modifies the global connection store.
	conn := newReuseTestConnection(t, 40002)

	EndConnectionForReusedTuple(conn.ID)

	if _, ok := GetConnection(conn.ID); ok {
		t.Fatal("connection is still in the state")
	}
	conn.Lock()
	ended := conn.Ended
	conn.Unlock()
	if ended == 0 || !conn.HasEnded() {
		t.Errorf("connection was not marked as ended (Ended=%d)", ended)
	}

	// Unknown IDs are ignored.
	EndConnectionForReusedTuple("17-10.0.2.15-1-10.0.2.3-53")
}
