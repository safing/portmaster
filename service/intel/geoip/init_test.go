package geoip

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/updates"
)

type testInstance struct {
	db      *dbmodule.DBModule
	api     *api.API
	config  *config.Config
	updates *updates.Updates
}

var _ instance = &testInstance{}

func (stub *testInstance) Updates() *updates.Updates {
	return stub.updates
}

func (stub *testInstance) API() *api.API {
	return stub.api
}

func (stub *testInstance) Config() *config.Config {
	return stub.config
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
	ds, err := config.InitializeUnitTestDataroot("test-geoip")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	stub := &testInstance{}
	stub.db, err = dbmodule.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	stub.config, err = config.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	stub.api, err = api.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create api: %w", err)
	}
	stub.updates, err = updates.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create updates: %w", err)
	}
	module, err = New(stub)
	if err != nil {
		return fmt.Errorf("failed to initialize module: %w", err)
	}

	err = stub.db.Start()
	if err != nil {
		return fmt.Errorf("Failed to start database: %w", err)
	}
	err = stub.config.Start()
	if err != nil {
		return fmt.Errorf("Failed to start config: %w", err)
	}
	err = stub.api.Start()
	if err != nil {
		return fmt.Errorf("Failed to start api: %w", err)
	}
	err = stub.updates.Start()
	if err != nil {
		return fmt.Errorf("Failed to start updates: %w", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("failed to start module: %w", err)
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
