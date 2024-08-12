package docks

import (
	"fmt"
	"os"
	"testing"

<<<<<<< HEAD
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
||||||| 151a548c
	"github.com/safing/portmaster/service/core/pmtesting"
=======
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
>>>>>>> develop
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/terminal"
)

<<<<<<< HEAD
type testInstance struct {
	db       *dbmodule.DBModule
	config   *config.Config
	metrics  *metrics.Metrics
	rng      *rng.Rng
	base     *base.Base
	access   *access.Access
	terminal *terminal.TerminalModule
	cabin    *cabin.Cabin
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
	ds, err := config.InitializeUnitTestDataroot("test-docks")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	instance := &testInstance{}
||||||| 151a548c
func TestMain(m *testing.M) {
=======
type testInstance struct {
	db       *dbmodule.DBModule
	config   *config.Config
	metrics  *metrics.Metrics
	rng      *rng.Rng
	base     *base.Base
	access   *access.Access
	terminal *terminal.TerminalModule
	cabin    *cabin.Cabin
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
	_ = log.Start()

	ds, err := config.InitializeUnitTestDataroot("test-docks")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	instance := &testInstance{}
>>>>>>> develop
	runningTests = true
	conf.EnablePublicHub(true) // Make hub config available.
	access.EnableTestMode()    // Register test zone instead of real ones.
<<<<<<< HEAD

	// Init
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
	instance.access, err = access.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create access module: %w\n", err)
	}
	instance.terminal, err = terminal.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create terminal module: %w\n", err)
	}
	instance.cabin, err = cabin.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create cabin module: %w\n", err)
	}
	module, err = New(instance)
	if err != nil {
		return fmt.Errorf("failed to create docks module: %w\n", err)
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
	err = instance.access.Start()
	if err != nil {
		return fmt.Errorf("failed to start access module: %w\n", err)
	}
	err = instance.terminal.Start()
	if err != nil {
		return fmt.Errorf("failed to start terminal module: %w\n", err)
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
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
||||||| 151a548c
	pmtesting.TestMain(m, module)
=======

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
	instance.access, err = access.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create access module: %w", err)
	}
	instance.terminal, err = terminal.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create terminal module: %w", err)
	}
	instance.cabin, err = cabin.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create cabin module: %w", err)
	}
	module, err = New(instance)
	if err != nil {
		return fmt.Errorf("failed to create docks module: %w", err)
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
	err = instance.access.Start()
	if err != nil {
		return fmt.Errorf("failed to start access module: %w", err)
	}
	err = instance.terminal.Start()
	if err != nil {
		return fmt.Errorf("failed to start terminal module: %w", err)
	}
	err = instance.cabin.Start()
	if err != nil {
		return fmt.Errorf("failed to start cabin module: %w", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("failed to start docks module: %w", err)
	}

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
>>>>>>> develop
}
