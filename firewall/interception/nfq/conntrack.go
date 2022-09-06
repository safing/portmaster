//go:build linux

package nfq

import (
	"encoding/binary"

	ct "github.com/florianl/go-conntrack"
)

// DeleteAllMarkedConnection deletes all marked entries from the conntrack table.
func DeleteAllMarkedConnection() error {
	nfct, err := ct.Open(&ct.Config{})
	if err != nil {
		return err
	}
	defer func() { _ = nfct.Close() }()

	// Delete all ipv4 marked connections
	connections := getAllMarkedConnections(nfct, ct.IPv4)
	for _, connection := range connections {
		_ = nfct.Delete(ct.Conntrack, ct.IPv4, connection)
	}

	// Delete all ipv6 marked connections
	connections = getAllMarkedConnections(nfct, ct.IPv6)
	for _, connection := range connections {
		_ = nfct.Delete(ct.Conntrack, ct.IPv6, connection)
	}

	return nil
}

func getAllMarkedConnections(nfct *ct.Nfct, f ct.Family) []ct.Con {
	// initialize variables
	permanentFlags := [...]uint32{MarkAccept, MarkBlock, MarkDrop, MarkAcceptAlways, MarkBlockAlways, MarkDropAlways, MarkRerouteNS, MarkRerouteSPN}
	filter := ct.FilterAttr{}
	filter.MarkMask = []byte{0xFF, 0xFF, 0xFF, 0xFF}
	filter.Mark = []byte{0x00, 0x00, 0x00, 0x00} // 4 zeros starting value
	connections := make([]ct.Con, 0)

	// get all connections from the specified family (ipv4 or ipv6)
	for _, mark := range permanentFlags {
		binary.BigEndian.PutUint32(filter.Mark, mark) // Little endian is in reverse not sure why. BigEndian makes it in correct order.
		currentConnections, err := nfct.Query(ct.Conntrack, f, filter)
		if err != nil {
			continue
		}
		connections = append(connections, currentConnections...)
	}

	return connections
}
