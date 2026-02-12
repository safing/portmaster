package interception

import (
	"errors"
	"flag"
	"os"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/updates"
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
	if err := prep(); err != nil {
		log.Errorf("Failed to prepare interception module %q", err)
		return err
	}

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

	// BindRequests is a stream of connection bind requests.
	// In use for split tunneling decisions.
	BindRequests = make(chan packet.BindRequest, 1000)

	disableInterception bool
	isStarted           atomic.Bool
)

func init() {
	flag.BoolVar(&disableInterception, "disable-interception", false, "disable packet interception; this breaks a lot of functionality")
}

func ensureSplitTunnelState() error {
	enabled := splitTunEnable()

	var err error = nil
	if enabled {
		err = EnableSplitTunnel(uint64(os.Getpid()))
	} else {
		err = DisableSplitTunnel()
	}

	if err != nil {
		log.Criticalf("failed to configure Split Tunneling state: %v", err)
		return err
	} else {
		log.Infof("Split Tunneling active: %v", enabled)
	}
	return nil
}

func prep() error {
	// Enable or disable split tunneling when the config changes
	module.instance.Config().EventConfigChange.AddCallback("split tunneling enable check", func(w *mgr.WorkerCtx, _ struct{}) (bool, error) {
		ensureSplitTunnelState()
		return false, nil
	})
	return nil
}

// Start starts the interception.
func start() error {
	if disableInterception {
		log.Warning("interception: packet interception is disabled via flag - this breaks a lot of functionality")
		return nil
	}

	if !isStarted.CompareAndSwap(false, true) {
		return nil // already running
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

	err := startInterception(inputPackets)

	if err == nil {
		// Enable Split Tunneling according to current config
		err = ensureSplitTunnelState()
	}

	if err != nil {
		log.Errorf("interception: failed to start module: %q", err)
		log.Debug("interception: cleaning up after failed start...")
		metrics.stop()
		if e := stopInterception(); e != nil {
			log.Debugf("interception: error cleaning up after failed start: %q", e.Error())
		}
		isStarted.Store(false)
	}

	return err
}

// Stop starts the interception.
func stop() error {
	if disableInterception {
		return nil
	}

	if !isStarted.CompareAndSwap(true, false) {
		return nil // not running
	}

	metrics.stop()
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

	if err := registerConfig(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	Config() *config.Config
	BinaryUpdates() *updates.Updater
}
