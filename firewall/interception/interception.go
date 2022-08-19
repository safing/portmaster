package interception

import (
	"flag"
	"fmt"

	ct "github.com/florianl/go-conntrack"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

var (
	// Packets channel for feeding the firewall.
	Packets = make(chan packet.Packet, 1000)

	disableInterception bool
)

func init() {
	flag.BoolVar(&disableInterception, "disable-interception", false, "disable packet interception; this breaks a lot of functionality")
}

// Start starts the interception.
func Start() error {
	if disableInterception {
		log.Warning("interception: packet interception is disabled via flag - this breaks a lot of functionality")
		return nil
	}

	inputPackets := Packets
	if packetMetricsDestination != "" {
		go metrics.writeMetrics()
		inputPackets = make(chan packet.Packet)
		go func() {
			for p := range inputPackets {
				Packets <- tracePacket(p)
			}
		}()
	}

	return start(inputPackets)
}

// Stop starts the interception.
func Stop() error {
	if disableInterception {
		return nil
	}

	close(metrics.done)

	return stop()
}

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
