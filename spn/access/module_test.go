package access

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database"
	_ "github.com/safing/portmaster/base/database/storage/hashmap"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

type testInstance struct {
	config *config.Config
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

func (stub *testInstance) IsShuttingDown() bool {
	return false
}

func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func (stub *testInstance) DataDir() string {
	return _dataDir
}

var _dataDir string

func TestMain(m *testing.M) {
	var err error
	// Create a temporary directory for the data
	_dataDir, err = os.MkdirTemp("", "")
	if err != nil {
		fmt.Printf("failed to create temporary data directory: %s", err)
		os.Exit(0)
	}
	defer func() { _ = os.RemoveAll(_dataDir) }()

	// Initialize the database module
	database.Initialize(_dataDir)
	_, err = database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: "hashmap",
	})
	if err != nil {
		fmt.Printf("failed to register core database: %s", err)
		os.Exit(0)
	}

	// Initialize the instance
	instance := &testInstance{}

	instance.config, err = config.New(instance)
	if err != nil {
		fmt.Printf("failed to create config module: %s", err)
		os.Exit(0)
	}
	module, err = New(instance)
	if err != nil {
		fmt.Printf("failed to create access module: %s", err)
		os.Exit(0)
	}

	err = instance.config.Start()
	if err != nil {
		fmt.Printf("failed to start config module: %s", err)
		os.Exit(0)
	}
	err = module.Start()
	if err != nil {
		fmt.Printf("failed to start access module: %s", err)
		os.Exit(0)
	}

	conf.EnableClient(true)
	m.Run()
}
