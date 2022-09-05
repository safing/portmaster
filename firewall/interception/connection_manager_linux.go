package interception

import (
	"encoding/binary"
	"fmt"
	"net"

	ct "github.com/florianl/go-conntrack"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall/interception/nfq"
)

// CloseAllConnections closes all active connection on conntrack.
func CloseAllConnections() error {
	nfct, err := ct.Open(&ct.Config{})
	if err != nil {
		return err
	}
	defer func() { _ = nfct.Close() }()

	connections, err := nfct.Dump(ct.Conntrack, ct.IPv4)
	if err != nil {
		return err
	}
	log.Criticalf("Number of connections: %d", len(connections))
	for _, connection := range connections {
		fmt.Printf("[%2d] %s - %s\n", connection.Origin.Proto.Number, connection.Origin.Src, connection.Origin.Dst)
		err := nfct.Delete(ct.Conntrack, ct.IPv4, connection)
		log.Errorf("Error deleting connection %q", err)
	}

	return nil
}

// DeleteAllConnections deletes all entries from conntrack table.
func DeleteAllConnections() error {
	nfct, err := ct.Open(&ct.Config{})
	if err != nil {
		return err
	}
	defer func() { _ = nfct.Close() }()

	connections, err := getAllPermanentConnections(nfct)

	for _, connection := range connections {
		_ = nfct.Delete(ct.Conntrack, ct.IPv4, connection)
	}

	return err
}

// DeleteConnection deletes a specific connection.
func DeleteConnection(sourceIP net.IP, sourcePort uint16, destinationIP net.IP, destinationPort uint16) error {
	nfct, err := ct.Open(&ct.Config{})
	if err != nil {
		return err
	}
	defer func() { _ = nfct.Close() }()

	filter := &ct.IPTuple{Src: &sourceIP, Dst: &destinationIP, Proto: &ct.ProtoTuple{SrcPort: &sourcePort, DstPort: &destinationPort}}
	connectionFilter := ct.Con{
		Origin: filter,
	}

	connections, _ := nfct.Get(ct.Conntrack, ct.IPv4, connectionFilter)
	for _, connection := range connections {
		_ = nfct.Delete(ct.Conntrack, ct.IPv4, connection)
	}

	connectionFilter.Origin = nil
	connectionFilter.Reply = filter
	connections, err = nfct.Get(ct.Conntrack, ct.IPv4, connectionFilter)
	for _, connection := range connections {
		_ = nfct.Delete(ct.Conntrack, ct.IPv4, connection)
	}
	return err
}

func getAllPermanentConnections(nfct *ct.Nfct) ([]ct.Con, error) {
	permanentFlags := []uint32{nfq.MarkAccept, nfq.MarkBlock, nfq.MarkDrop, nfq.MarkAcceptAlways, nfq.MarkBlockAlways, nfq.MarkDropAlways, nfq.MarkRerouteSPN}
	filter := ct.FilterAttr{}
	filter.MarkMask = []byte{0xFF, 0xFF, 0xFF, 0xFF}
	filter.Mark = []byte{0x00, 0x00, 0x00, 0x00} // 4 zeros starting value
	connections := make([]ct.Con, 0)
	for _, mark := range permanentFlags {
		binary.BigEndian.PutUint32(filter.Mark, mark) // Little endian is in reverse not sure why. BigEndian makes it in correct order.
		currentConnections, err := nfct.Query(ct.Conntrack, ct.IPv4, filter)
		if err != nil {
			return nil, err
		}
		connections = append(connections, currentConnections...)
	}
	return connections, nil
}
