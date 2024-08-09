package interception

import (
	"errors"
	"flag"
	"sync/atomic"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
)

// Interception is the packet interception module.
type Interception struct {
	mgr      *mgr.Manager
	instance instance
}

// Manager returns the module manager.
func (i *Interception) Manager() *mgr.Manager {
	return i.mgr
}

// Start starts the module.
func (i *Interception) Start() error {
	return start()
}

// Stop stops the module.
func (i *Interception) Stop() error {
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
	if err := stopInterception(); err != nil {
		log.Errorf("failed to stop interception module: %s", err)
	}
	return nil
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
	m := mgr.New("Interception")
	module = &Interception{
		mgr:      m,
		instance: instance,
	}
	return module, nil
}

type instance interface{}
