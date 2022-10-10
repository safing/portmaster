//go:build linux

package nfq

import (
	"encoding/binary"

	ct "github.com/florianl/go-conntrack"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
)

// DeleteAllMarkedConnection deletes all marked entries from the conntrack table.
func DeleteAllMarkedConnection() error {
	nfct, err := ct.Open(&ct.Config{})
	if err != nil {
		return err
	}
	defer func() { _ = nfct.Close() }()

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
	permanentFlags := []uint32{MarkAcceptAlways, MarkBlockAlways, MarkDropAlways, MarkRerouteNS, MarkRerouteSPN}
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
			deleteError = nfct.Delete(ct.Conntrack, ct.IPv4, connection)
			if err != nil {
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
