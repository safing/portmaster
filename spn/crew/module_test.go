package crew

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/terminal"
)

type testInstance struct {
	db        *dbmodule.DBModule
	config    *config.Config
	metrics   *metrics.Metrics
	rng       *rng.Rng
	base      *base.Base
	terminal  *terminal.TerminalModule
	cabin     *cabin.Cabin
	docks     *docks.Docks
	geoip     *geoip.GeoIP
	navigator *navigator.Navigator

	// updates is an updater without any index. It makes geoip lookups fail
	// cleanly, as no geoip databases are available in tests.
	updates *updates.Updater
}

func (stub *testInstance) BinaryUpdates() *updates.Updater {
	return stub.updates
}

func (stub *testInstance) IntelUpdates() *updates.Updater {
	return stub.updates
}

func (stub *testInstance) Config() *config.Config {
	return stub.config
}

func (stub *testInstance) Metrics() *metrics.Metrics {
	return stub.metrics
}

func (stub *testInstance) SPNGroup() *mgr.ExtendedGroup {
	return nil
}

func (stub *testInstance) Stopping() bool {
	return false
}

func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func (stub *testInstance) DataDir() string {
	return _dataDir
}

var _dataDir string

func runTest(m *testing.M) error {
	conf.EnablePublicHub(true) // Make hub config available.
	access.EnableTestMode()    // Register test zone instead of real ones.
	// Do not probe the network (eg. with traceroute) for the device location,
	// which the navigator requests when adding hubs to the main map.
	netenv.DisableNetworkDerivedLocation(true)

	var err error
	// Create a temporary directory for the data
	_dataDir, err = os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(_dataDir) }()

	instance := &testInstance{
		updates: &updates.Updater{},
	}
	// Init
	instance.db, err = dbmodule.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create database module: %w", err)
	}
	instance.config, err = config.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create config module: %w", err)
	}
	instance.metrics, err = metrics.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create metrics module: %w", err)
	}
	instance.rng, err = rng.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create rng module: %w", err)
	}
	instance.base, err = base.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create base module: %w", err)
	}
	instance.terminal, err = terminal.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create terminal module: %w", err)
	}
	instance.cabin, err = cabin.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create cabin module: %w", err)
	}
	instance.docks, err = docks.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create docks module: %w", err)
	}
	// The geoip module is only created, not started, as there are no geoip
	// databases. The navigator needs it to look up (missing) locations.
	instance.geoip, err = geoip.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create geoip module: %w", err)
	}
	instance.navigator, err = navigator.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create navigator module: %w", err)
	}
	module, err = New(instance)
	if err != nil {
		return fmt.Errorf("failed to create crew module: %w", err)
	}

	// Start
	err = instance.db.Start()
	if err != nil {
		return fmt.Errorf("failed to start db module: %w", err)
	}
	err = instance.config.Start()
	if err != nil {
		return fmt.Errorf("failed to start config module: %w", err)
	}
	err = instance.metrics.Start()
	if err != nil {
		return fmt.Errorf("failed to start metrics module: %w", err)
	}
	err = instance.rng.Start()
	if err != nil {
		return fmt.Errorf("failed to start rng module: %w", err)
	}
	err = instance.base.Start()
	if err != nil {
		return fmt.Errorf("failed to start base module: %w", err)
	}
	err = instance.terminal.Start()
	if err != nil {
		return fmt.Errorf("failed to start terminal module: %w", err)
	}
	err = instance.cabin.Start()
	if err != nil {
		return fmt.Errorf("failed to start cabin module: %w", err)
	}
	err = instance.docks.Start()
	if err != nil {
		return fmt.Errorf("failed to start docks module: %w", err)
	}
	err = instance.navigator.Start()
	if err != nil {
		return fmt.Errorf("failed to start navigator module: %w", err)
	}
	// Hubs are only regarded for routing after the intel data was applied.
	// Use empty intel data, so that no intel based states are set.
	mapIntel := &hub.Intel{}
	err = mapIntel.ParseAdvisories()
	if err != nil {
		return fmt.Errorf("failed to parse map intel: %w", err)
	}
	err = navigator.Main.UpdateIntel(mapIntel, nil)
	if err != nil {
		return fmt.Errorf("failed to set map intel: %w", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("failed to start crew module: %w", err)
	}
	// Initialize the access zones without the access module, which would also
	// manage the account and request tokens.
	err = access.InitializeZones()
	if err != nil {
		return fmt.Errorf("failed to initialize access zones: %w", err)
	}

	conf.EnablePublicHub(true)
	m.Run()

	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}
}
