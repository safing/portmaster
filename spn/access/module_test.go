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

func (stub *testInstance) Config() *config.Config             { return stub.config }
func (stub *testInstance) SPNGroup() *mgr.ExtendedGroup       { return nil }
func (stub *testInstance) Stopping() bool                     { return false }
func (stub *testInstance) IsShuttingDown() bool               { return false }
func (stub *testInstance) SetCmdLineOperation(f func() error) {}
func (stub *testInstance) DataDir() string                    { return _dataDir }

var _dataDir string

func TestMain(m *testing.M) {
	exitCode := 1
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	var err error
	// Create a temporary directory for the data
	_dataDir, err = os.MkdirTemp("", "")
	if err != nil {
		fmt.Printf("failed to create temporary data directory: %s", err)
		return // Exit with error
	}
	defer func() { _ = os.RemoveAll(_dataDir) }()

	// Initialize the database module
	err = database.Initialize(_dataDir)
	if err != nil {
		fmt.Printf("failed to initialize database module: %s", err)
		return // Exit with error
	}
	_, err = database.Register(&database.Database{
		Name:        "core",
		Description: "Holds core data, such as settings and profiles",
		StorageType: "hashmap",
	})
	if err != nil {
		fmt.Printf("failed to register core database: %s", err)
		return // Exit with error
	}

	// Initialize the instance
	instance := &testInstance{}

	instance.config, err = config.New(instance)
	if err != nil {
		fmt.Printf("failed to create config module: %s", err)
		return // Exit with error
	}
	module, err = New(instance)
	if err != nil {
		fmt.Printf("failed to create access module: %s", err)
		return // Exit with error
	}

	err = instance.config.Start()
	if err != nil {
		fmt.Printf("failed to start config module: %s", err)
		return // Exit with error
	}
	err = module.Start()
	if err != nil {
		fmt.Printf("failed to start access module: %s", err)
		return // Exit with error
	}

	conf.EnableClient(true)
	m.Run()

	exitCode = 0 //success
}
