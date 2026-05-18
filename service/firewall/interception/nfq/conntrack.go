//go:build linux

package nfq

import (
	"encoding/binary"
	"errors"
	"fmt"

	ct "github.com/florianl/go-conntrack"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	pmpacket "github.com/safing/portmaster/service/network/packet"
)

var nfct *ct.Nfct // Conntrack handler. NFCT: Network Filter Connection Tracking.

// InitNFCT initializes the network filter conntrack library.
func InitNFCT() error {
	var err error
	nfct, err = ct.Open(&ct.Config{})
	if err != nil {
		return err
	}
	return nil
}

// TeardownNFCT deinitializes the network filter conntrack library.
func TeardownNFCT() {
	if nfct != nil {
		_ = nfct.Close()
	}
}

// DeleteUnmarkedConnections deletes all conntrack entries with connmark=0,
// excluding loopback connections.
// These entries represent connections established while Portmaster was not
// running or was paused and therefore never received a verdict mark.
//
// The Linux netfilter nat table applies DNAT only to the first packet of a NEW
// connection. ESTABLISHED connections bypass the nat table entirely, so any
// routing decision (e.g. MarkRerouteSPN, MarkRerouteSplitTun) would never take
// effect for them. Removing their conntrack entries forces applications to
// reconnect; the resulting SYN is processed by NFQUEUE as a new connection and
// the correct DNAT rule fires.
//
// Loopback connections (source or destination is 127.x.x.x / ::1) are skipped.
// They always carry connmark=0 because Portmaster never saves a permanent mark
// for loopback-destined packets. Flushing them would needlessly disconnect apps
// talking to local services (databases, dev servers, local APIs, etc.).
//
// Connections already processed by Portmaster carry a non-zero connmark and
// are handled via CONNMARK --restore-mark; they are unaffected.
func DeleteUnmarkedConnections() error {
	if nfct == nil {
		return errors.New("nfq: nfct not initialized")
	}

	deleted := deleteUnmarkedConnections(nfct, ct.IPv4)

	if netenv.IPv6Enabled() {
		deleted += deleteUnmarkedConnections(nfct, ct.IPv6)
	}

	log.Infof("nfq: deleted %d unmarked conntrack entries to force re-evaluation on firewall activation", deleted)
	return nil
}

func deleteUnmarkedConnections(nfct *ct.Nfct, f ct.Family) (deleted int) {
	filter := ct.FilterAttr{
		Mark:     []byte{0x00, 0x00, 0x00, 0x00},
		MarkMask: []byte{0xFF, 0xFF, 0xFF, 0xFF},
	}

	connections, err := nfct.Query(ct.Conntrack, f, filter)
	if err != nil {
		log.Warningf("nfq: error querying unmarked conntrack entries: %s", err)
		return 0
	}

	var lastErr error
	for _, connection := range connections {
		if isLoopbackConnection(connection) {
			continue
		}
		if err := nfct.Delete(ct.Conntrack, f, connection); err != nil {
			lastErr = err
		} else {
			deleted++
		}
	}

	if lastErr != nil {
		log.Warningf("nfq: some unmarked conntrack entries could not be deleted, last error: %s", lastErr)
	}
	return deleted
}

// isLoopbackConnection reports whether a conntrack entry involves a loopback address.
func isLoopbackConnection(c ct.Con) bool {
	if c.Origin != nil {
		if c.Origin.Src != nil && c.Origin.Src.IsLoopback() {
			return true
		}
		if c.Origin.Dst != nil && c.Origin.Dst.IsLoopback() {
			return true
		}
	}
	if c.Reply != nil {
		if c.Reply.Src != nil && c.Reply.Src.IsLoopback() {
			return true
		}
		if c.Reply.Dst != nil && c.Reply.Dst.IsLoopback() {
			return true
		}
	}
	return false
}

// DeleteAllMarkedConnection deletes all marked entries from the conntrack table.
func DeleteAllMarkedConnection() error {
	if nfct == nil {
		return errors.New("nfq: nfct not initialized")
	}

	// Delete all ipv4 marked connections
	deleted := deleteMarkedConnections(nfct, ct.IPv4)

	if netenv.IPv6Enabled() {
		// Delete all ipv6 marked connections
		deleted += deleteMarkedConnections(nfct, ct.IPv6)
	}

	log.Infof("nfq: deleted %d conntrack entries to reset permanent connection verdicts", deleted)
	return nil
}

func deleteMarkedConnections(nfct *ct.Nfct, f ct.Family) (deleted int) {
	// initialize variables
	permanentFlags := []uint32{MarkAcceptAlways, MarkBlockAlways, MarkDropAlways, MarkRerouteNS, MarkRerouteSPN, MarkRerouteSplitTun}
	filter := ct.FilterAttr{}
	filter.MarkMask = []byte{0xFF, 0xFF, 0xFF, 0xFF}
	filter.Mark = []byte{0x00, 0x00, 0x00, 0x00} // 4 zeros starting value

	numberOfErrors := 0
	var deleteError error = nil
	// Get all connections from the specified family (ipv4 or ipv6)
	for _, mark := range permanentFlags {
		binary.BigEndian.PutUint32(filter.Mark, mark) // Little endian is in reverse not sure why. BigEndian makes it in correct order.
		currentConnections, err := nfct.Query(ct.Conntrack, f, filter)
		if err != nil {
			log.Warningf("nfq: error on conntrack query: %s", err)
			continue
		}

		for _, connection := range currentConnections {
			deleteError = nfct.Delete(ct.Conntrack, f, connection)
			if deleteError != nil {
				numberOfErrors++
			} else {
				deleted++
			}
		}
	}

	if numberOfErrors > 0 {
		log.Warningf("nfq: failed to delete %d conntrack entries last error is: %s", numberOfErrors, deleteError)
	}
	return deleted
}

// DeleteMarkedConnection removes a specific connection from the conntrack table.
func DeleteMarkedConnection(conn *network.Connection) error {
	if nfct == nil {
		return errors.New("nfq: nfct not initialized")
	}

	con := ct.Con{
		Origin: &ct.IPTuple{
			Src: &conn.LocalIP,
			Dst: &conn.Entity.IP,
			Proto: &ct.ProtoTuple{
				Number:  &conn.Entity.Protocol,
				SrcPort: &conn.LocalPort,
				DstPort: &conn.Entity.Port,
			},
		},
	}

	family := ct.IPv4
	if conn.IPVersion == pmpacket.IPv6 {
		family = ct.IPv6
	}

	connections, err := nfct.Get(ct.Conntrack, family, con)
	if err != nil {
		return fmt.Errorf("nfq: failed to find entry for connection %s: %w", conn.String(), err)
	}

	if len(connections) > 1 {
		log.Warningf("nfq: multiple entries found for single connection: %s -> %d", conn.String(), len(connections))
	}

	for _, connection := range connections {
		deleteErr := nfct.Delete(ct.Conntrack, family, connection)
		if err == nil {
			err = deleteErr
		}
	}

	if err != nil {
		log.Warningf("nfq: error while deleting conntrack entries for connection %s: %s", conn.String(), err)
	}

	return nil
}
