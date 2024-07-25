package terminal

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/conf"
)

type testInstance struct {
	db      *dbmodule.DBModule
	config  *config.Config
	metrics *metrics.Metrics
	rng     *rng.Rng
	base    *base.Base
	cabin   *cabin.Cabin
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

func runTest(m *testing.M) error {
	ds, err := config.InitializeUnitTestDataroot("test-terminal")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	conf.EnablePublicHub(true) // Make hub config available.

	instance := &testInstance{}
	instance.db, err = dbmodule.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create database module: %w\n", err)
	}
	instance.config, err = config.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create config module: %w\n", err)
	}
	instance.metrics, err = metrics.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create metrics module: %w\n", err)
	}
	instance.rng, err = rng.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create rng module: %w\n", err)
	}
	instance.base, err = base.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create base module: %w\n", err)
	}
	instance.cabin, err = cabin.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create cabin module: %w\n", err)
	}
	_, err = New(instance)
	if err != nil {
		fmt.Printf("failed to create module: %s\n", err)
		os.Exit(0)
	}

	// Start
	err = instance.db.Start()
	if err != nil {
		return fmt.Errorf("failed to start db module: %w\n", err)
	}
	err = instance.config.Start()
	if err != nil {
		return fmt.Errorf("failed to start config module: %w\n", err)
	}
	err = instance.metrics.Start()
	if err != nil {
		return fmt.Errorf("failed to start metrics module: %w\n", err)
	}
	err = instance.rng.Start()
	if err != nil {
		return fmt.Errorf("failed to start rng module: %w\n", err)
	}
	err = instance.base.Start()
	if err != nil {
		return fmt.Errorf("failed to start base module: %w\n", err)
	}
	err = instance.cabin.Start()
	if err != nil {
		return fmt.Errorf("failed to start cabin module: %w\n", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("failed to start docks module: %w\n", err)
	}

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		os.Exit(1)
	}
}
