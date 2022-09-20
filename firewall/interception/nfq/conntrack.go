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
	deleteMarkedConnections(nfct, ct.IPv4)

	if netenv.IPv6Enabled() {
		// Delete all ipv6 marked connections
		deleteMarkedConnections(nfct, ct.IPv6)
	}

	return nil
}

func deleteMarkedConnections(nfct *ct.Nfct, f ct.Family) {
	// initialize variables
	permanentFlags := [...]uint32{MarkAccept, MarkBlock, MarkDrop, MarkAcceptAlways, MarkBlockAlways, MarkDropAlways, MarkRerouteNS, MarkRerouteSPN}
	filter := ct.FilterAttr{}
	filter.MarkMask = []byte{0xFF, 0xFF, 0xFF, 0xFF}
	filter.Mark = []byte{0x00, 0x00, 0x00, 0x00} // 4 zeros starting value

	// get all connections from the specified family (ipv4 or ipv6)
	for _, mark := range permanentFlags {
		binary.BigEndian.PutUint32(filter.Mark, mark) // Little endian is in reverse not sure why. BigEndian makes it in correct order.
		currentConnections, err := nfct.Query(ct.Conntrack, f, filter)
		if err != nil {
			log.Warningf("nfq: error on conntrack query: %s", err)
			continue
		}

		numberOfErrors := 0
		for _, connection := range currentConnections {
			err = nfct.Delete(ct.Conntrack, ct.IPv4, connection)
			if err != nil {
				numberOfErrors++
			}
		}

		if numberOfErrors > 0 {
			log.Warningf("nfq: failed to delete %d conntrack entries last error is: %s", numberOfErrors, err)
		}
	}
}
