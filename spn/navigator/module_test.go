package navigator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/ui"
	"github.com/safing/portmaster/service/updates"
)

type testInstance struct {
	db           *dbmodule.DBModule
	config       *config.Config
	intelUpdates *updates.Updater
	geoip        *geoip.GeoIP
}

func (stub *testInstance) IntelUpdates() *updates.Updater              { return stub.intelUpdates }
func (stub *testInstance) Config() *config.Config                      { return stub.config }
func (stub *testInstance) Notifications() *notifications.Notifications { return nil }
func (stub *testInstance) Ready() bool                                 { return true }
func (stub *testInstance) Restart()                                    {}
func (stub *testInstance) Shutdown()                                   {}
func (stub *testInstance) SetCmdLineOperation(f func() error)          {}
func (stub *testInstance) BinaryUpdates() *updates.Updater             { return nil }
func (stub *testInstance) UI() *ui.UI                                  { return nil }
func (stub *testInstance) DataDir() string                             { return _dataDir }

var _dataDir string

func runTest(m *testing.M) error {
	var err error

	// Create a temporary directory for testing
	_dataDir, err = os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("failed to create temporary data directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(_dataDir) }()

	// Initialize the Intel update configuration
	intelUpdateConfig := updates.Config{
		Name:              configure.DefaultIntelIndexName,
		Directory:         filepath.Join(_dataDir, "test_intel"),
		DownloadDirectory: filepath.Join(_dataDir, "test_download_intel"),
		PurgeDirectory:    filepath.Join(_dataDir, "test_upgrade_obsolete_intel"),
		IndexURLs:         configure.DefaultIntelIndexURLs,
		IndexFile:         "index.json",
		AutoCheck:         true,
		AutoDownload:      true,
		AutoApply:         true,
	}

	// Set the default API listen address
	api.SetDefaultAPIListenAddress("0.0.0.0:8080")

	// Initialize the instance with the necessary components
	stub := &testInstance{}
	log.SetLogLevel(log.DebugLevel)

	// Init
	stub.db, err = dbmodule.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create db: %w", err)
	}
	stub.config, err = config.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	stub.intelUpdates, err = updates.New(stub, "Intel Updater", intelUpdateConfig)
	if err != nil {
		return fmt.Errorf("failed to create updates: %w", err)
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
	err = stub.config.Start()
	if err != nil {
		return fmt.Errorf("failed to start config: %w", err)
	}
	err = stub.intelUpdates.Start()
	if err != nil {
		return fmt.Errorf("failed to start updates: %w", err)
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
