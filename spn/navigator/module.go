package navigator

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

const (
	// cfgOptionRoutingAlgorithmKey is copied from profile/config.go to avoid import loop.
	cfgOptionRoutingAlgorithmKey = "spn/routingAlgorithm"

	// cfgOptionRoutingAlgorithmKey is copied from captain/config.go to avoid import loop.
	cfgOptionTrustNodeNodesKey = "spn/trustNodes"
)

var (
	// ErrHomeHubUnset is returned when the Home Hub is required and not set.
	ErrHomeHubUnset = errors.New("map has no Home Hub set")

	// ErrEmptyMap is returned when the Map is empty.
	ErrEmptyMap = errors.New("map is empty")

	// ErrHubNotFound is returned when the Hub was not found on the Map.
	ErrHubNotFound = errors.New("hub not found")

	// ErrAllPinsDisregarded is returned when all pins have been disregarded.
	ErrAllPinsDisregarded = errors.New("all pins have been disregarded")
)

type Navigator struct {
	mgr *mgr.Manager

	instance instance
}

func (n *Navigator) Manager() *mgr.Manager {
	return n.mgr
}

func (n *Navigator) Start() error {
	return start()
}

func (n *Navigator) Stop() error {
	return stop()
}

var (
	module     *Navigator
	shimLoaded atomic.Bool

	// Main is the primary map used.
	Main *Map

	devMode                   config.BoolOption
	cfgOptionRoutingAlgorithm config.StringOption
	cfgOptionTrustNodeNodes   config.StringArrayOption
)

func prep() error {
	return registerAPIEndpoints()
}

func start() error {
	Main = NewMap(conf.MainMapName, true)
	devMode = config.Concurrent.GetAsBool(config.CfgDevModeKey, false)
	cfgOptionTrustNodeNodes = config.Concurrent.GetAsStringArray(cfgOptionTrustNodeNodesKey, []string{})

	if conf.Integrated() {
		cfgOptionRoutingAlgorithm = config.Concurrent.GetAsString(cfgOptionRoutingAlgorithmKey, DefaultRoutingProfileID)
	} else {
		cfgOptionRoutingAlgorithm = func() string { return DefaultRoutingProfileID }
	}

	err := registerMapDatabase()
	if err != nil {
		return err
	}

	module.mgr.Go("initializing hubs", func(wc *mgr.WorkerCtx) error {
		// Wait for geoip databases to be ready.
		// Try again if not yet ready, as this is critical.
		// The "wait" parameter times out after 1 second.
		// Allow 30 seconds for both databases to load.
	geoInitCheck:
		for range 30 {
			switch {
			case !geoip.IsInitialized(false, true): // First, IPv4.
			case !geoip.IsInitialized(true, true): // Then, IPv6.
			default:
				break geoInitCheck
			}
		}

		err = Main.InitializeFromDatabase()
		if err != nil {
			// Wait for three seconds, then try again.
			time.Sleep(3 * time.Second)
			err = Main.InitializeFromDatabase()
			if err != nil {
				// Even if the init fails, we can try to start without it and get data along the way.
				log.Warningf("spn/navigator: %s", err)
			}
		}
		err = Main.RegisterHubUpdateHook()
		if err != nil {
			return err
		}

		// TODO: delete superseded hubs after x amount of time
		_ = module.mgr.Delay("update states", 3*time.Minute, Main.updateStates).Repeat(1 * time.Hour)
		_ = module.mgr.Delay("update failing states", 3*time.Minute, Main.updateFailingStates).Repeat(1 * time.Minute)

		if conf.PublicHub() {
			// Only measure Hubs on public Hubs.
			module.mgr.Delay("measure hubs", 5*time.Minute, Main.measureHubs).Repeat(1 * time.Minute)

			// Only register metrics on Hubs, as they only make sense there.
			err := registerMetrics()
			if err != nil {
				return err
			}
		}
		return nil
	})

	return nil
}

func stop() error {
	withdrawMapDatabase()

	Main.CancelHubUpdateHook()
	Main.SaveMeasuredHubs()
	Main.Close()

	return nil
}

// New returns a new Navigator module.
func New(instance instance) (*Navigator, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Navigator")
	module = &Navigator{
		mgr:      m,
		instance: instance,
	}
	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
