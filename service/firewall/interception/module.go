package interception

import (
	"errors"
	"flag"
	"sync/atomic"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
)

type Interception struct {
	mgr      *mgr.Manager
	instance instance
}

func (i *Interception) Start(m *mgr.Manager) error {
	i.mgr = m
	return start()
}

func (i *Interception) Stop(m *mgr.Manager) error {
	return stop()
}

var (
	// Packets is a stream of interception network packets.
	Packets = make(chan packet.Packet, 1000)

	// BandwidthUpdates is a stream of bandwidth usage update for connections.
	BandwidthUpdates = make(chan *packet.BandwidthUpdate, 1000)

	disableInterception bool
)

func init() {
	flag.BoolVar(&disableInterception, "disable-interception", false, "disable packet interception; this breaks a lot of functionality")

	// module = modules.Register("interception", prep, start, stop, "base", "updates", "network", "notifications", "profiles")
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

var (
	module     *Interception
	shimLoaded atomic.Bool
)

// New returns a new Interception module.
func New(instance instance) (*Interception, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Interception{
		instance: instance,
	}
	return module, nil
}

type instance interface{}
