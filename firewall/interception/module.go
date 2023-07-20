package interception

import (
	"flag"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/network/packet"
)

var (
	module *modules.Module

	// Packets is a stream of interception network packest.
	Packets = make(chan packet.Packet, 1000)

	// BandwidthUpdates is a stream of bandwidth usage update for connections.
	BandwidthUpdates = make(chan *packet.BandwidthUpdate, 1000)

	disableInterception bool
)

func init() {
	flag.BoolVar(&disableInterception, "disable-interception", false, "disable packet interception; this breaks a lot of functionality")

	module = modules.Register("interception", prep, start, stop, "base", "updates", "network", "notifications", "profiles")
}

func prep() error {
	return nil
}

// Start starts the interception.
func start() error {
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

	return startInterception(inputPackets)
}

// Stop starts the interception.
func stop() error {
	if disableInterception {
		return nil
	}

	close(metrics.done)

	return stopInterception()
}
