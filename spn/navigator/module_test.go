package navigator

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/updates"
)

type testInstance struct {
	db      *dbmodule.DBModule
	api     *api.API
	config  *config.Config
	updates *updates.Updates
	base    *base.Base
	geoip   *geoip.GeoIP
}

func (stub *testInstance) Updates() *updates.Updates {
	return stub.updates
}

func (stub *testInstance) API() *api.API {
	return stub.api
}

func (stub *testInstance) Config() *config.Config {
	return stub.config
}

func (stub *testInstance) Base() *base.Base {
	return stub.base
}

func (stub *testInstance) Notifications() *notifications.Notifications {
	return nil
}

func (stub *testInstance) Ready() bool {
	return true
}

func (stub *testInstance) Restart() {}

func (stub *testInstance) Shutdown() {}

func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func runTest(m *testing.M) error {
	api.SetDefaultAPIListenAddress("0.0.0.0:8080")
	ds, err := config.InitializeUnitTestDataroot("test-navigator")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	stub := &testInstance{}
	log.SetLogLevel(log.DebugLevel)

	// Init
	stub.db, err = dbmodule.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create db: %w", err)
	}
	stub.api, err = api.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create api: %w", err)
	}
	stub.config, err = config.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	stub.updates, err = updates.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create updates: %w", err)
	}
	stub.base, err = base.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create base: %w", err)
	}
	stub.geoip, err = geoip.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create geoip: %w", err)
	}
	module, err = New(stub)
	if err != nil {
		return fmt.Errorf("failed to create navigator module: %w", err)
	}
	// Start
	err = stub.db.Start()
	if err != nil {
		return fmt.Errorf("failed to start db module: %w", err)
	}
	err = stub.api.Start()
	if err != nil {
		return fmt.Errorf("failed to start api: %w", err)
	}
	err = stub.config.Start()
	if err != nil {
		return fmt.Errorf("failed to start config: %w", err)
	}
	err = stub.updates.Start()
	if err != nil {
		return fmt.Errorf("failed to start updates: %w", err)
	}
	err = stub.base.Start()
	if err != nil {
		return fmt.Errorf("failed to start base module: %w", err)
	}
	err = stub.geoip.Start()
	if err != nil {
		return fmt.Errorf("failed to start geoip module: %w", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("failed to start navigator module: %w", err)
	}

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
}
