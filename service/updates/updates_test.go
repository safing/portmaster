package updates

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/safing/portmaster/base/notifications"
)

type testInstance struct{}

func (i *testInstance) Restart()  {}
func (i *testInstance) Shutdown() {}

func (i *testInstance) Notifications() *notifications.Notifications {
	return nil
}

func (i *testInstance) Ready() bool {
	return true
}

func (i *testInstance) SetCmdLineOperation(f func() error) {}

func TestPreformUpdate(t *testing.T) {
	t.Parallel()

	// Initialize mock instance
	stub := &testInstance{}

	// Make tmp dirs
	installedDir, err := os.MkdirTemp("", "updates_current")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(installedDir) }()
	updateDir, err := os.MkdirTemp("", "updates_new")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(updateDir) }()
	purgeDir, err := os.MkdirTemp("", "updates_purge")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(purgeDir) }()

	// Generate mock files
	if err := GenerateMockFolder(installedDir, "Test", "1.0.0"); err != nil {
		panic(err)
	}
	if err := GenerateMockFolder(updateDir, "Test", "1.0.1"); err != nil {
		panic(err)
	}

	// Create updater
	updates, err := New(stub, "Test", UpdateIndex{
		Directory:         installedDir,
		DownloadDirectory: updateDir,
		PurgeDirectory:    purgeDir,
		IndexFile:         "index.json",
		AutoApply:         false,
		NeedsRestart:      false,
	})
	if err != nil {
		panic(err)
	}
	// Read and parse the index file
	if err := updates.downloader.Verify(); err != nil {
		panic(err)
	}
	// Try to apply the updates
	err = updates.applyUpdates(nil)
	if err != nil {
		panic(err)
	}

	// CHeck if the current version is now the new.
	bundle, err := LoadBundle(filepath.Join(installedDir, "index.json"))
	if err != nil {
		panic(err)
	}

	if bundle.Version != "1.0.1" {
		panic(fmt.Errorf("expected version 1.0.1 found %s", bundle.Version))
	}
}
