package cabin

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

type testInstance struct {
	db     *dbmodule.DBModule
	api    *api.API
	config *config.Config
	rng    *rng.Rng
	base   *base.Base
}

func (stub *testInstance) Config() *config.Config {
	return stub.config
}

func (stub *testInstance) SPNGroup() *mgr.ExtendedGroup {
	return nil
}

func (stub *testInstance) Stopping() bool {
	return false
}

func (stub *testInstance) Ready() bool {
	return true
}
func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func runTest(m *testing.M) error {
	api.SetDefaultAPIListenAddress("0.0.0.0:8080")
	// Initialize dataroot
	ds, err := config.InitializeUnitTestDataroot("test-cabin")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	// Init
	instance := &testInstance{}
	instance.db, err = dbmodule.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	instance.api, err = api.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create api: %w", err)
	}
	instance.config, err = config.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create config module: %w", err)
	}
	instance.rng, err = rng.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create rng module: %w", err)
	}
	instance.base, err = base.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create base module: %w", err)
	}
	module, err = New(struct{}{})
	if err != nil {
		return fmt.Errorf("failed to create cabin module: %w", err)
	}
	// Start
	err = instance.db.Start()
	if err != nil {
		return fmt.Errorf("failed to start database: %w", err)
	}
	err = instance.api.Start()
	if err != nil {
		return fmt.Errorf("failed to start api: %w", err)
	}
	err = instance.config.Start()
	if err != nil {
		return fmt.Errorf("failed to start config module: %w", err)
	}
	err = instance.rng.Start()
	if err != nil {
		return fmt.Errorf("failed to start rng module: %w", err)
	}
	err = instance.base.Start()
	if err != nil {
		return fmt.Errorf("failed to start base module: %w", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("failed to start cabin module: %w", err)
	}
	conf.EnablePublicHub(true)

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
}
