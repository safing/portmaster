package updates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/ui"
)

type testInstance struct{}

func (i *testInstance) Restart()                                    {}
func (i *testInstance) Shutdown()                                   {}
func (i *testInstance) Notifications() *notifications.Notifications { return nil }
func (i *testInstance) Ready() bool                                 { return true }
func (i *testInstance) SetCmdLineOperation(f func() error)          {}
func (i *testInstance) UI() *ui.UI                                  { return nil }

func TestPerformUpdate(t *testing.T) {
	t.Parallel()

	// Initialize mock instance
	stub := &testInstance{}

	// Make tmp dirs
	installedDir, err := os.MkdirTemp("", "updates_current_")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(installedDir) }()

	updateDir, err := os.MkdirTemp("", "updates_new_")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(updateDir) }()

	purgeDir, err := os.MkdirTemp("", "updates_purge_")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(purgeDir) }()

	// Generate mock files
	now := time.Now()
	if err := GenerateMockFolder(installedDir, "Test", "1.0.0", now); err != nil {
		t.Fatal(err)
	}
	if err := GenerateMockFolder(updateDir, "Test", "1.0.1", now.Add(1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Create updater (loads index).
	updater, err := New(stub, "Test", Config{
		Name:              "Test",
		Directory:         installedDir,
		DownloadDirectory: updateDir,
		PurgeDirectory:    purgeDir,
		IndexFile:         "index.json",
		AutoDownload:      true,
		AutoApply:         true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Try to apply the updates
	m := mgr.New("updates test")
	_ = m.Do("test update and upgrade", func(w *mgr.WorkerCtx) error {
		if err := updater.updateAndUpgrade(w, nil, false, false); err != nil {
			if data, err := os.ReadFile(filepath.Join(installedDir, "index.json")); err == nil {
				fmt.Println(string(data))
				fmt.Println(updater.index.Version)
				fmt.Println(updater.index.versionNum)
			}
			if data, err := os.ReadFile(filepath.Join(updateDir, "index.json")); err == nil {
				fmt.Println(string(data))
				idx, err := ParseIndex(data, updater.cfg.Platform, nil)
				if err == nil {
					fmt.Println(idx.Version)
					fmt.Println(idx.versionNum)
				}
			}

			t.Fatal(err)
		}
		return nil
	})

	// Check if the current version is now the new.
	newIndex, err := LoadIndex(filepath.Join(installedDir, "index.json"), updater.cfg.Platform, nil)
	if err != nil {
		t.Fatal(err)
	}
	if newIndex.Version != "1.0.1" {
		t.Fatalf("expected version 1.0.1 found %s", newIndex.Version)
	}
}

func TestPerformUpdateRepairsCorruptedResourcesAtSameVersion(t *testing.T) {
	t.Parallel()

	stub := &testInstance{}
	installedDir := t.TempDir()
	updateDir := t.TempDir()
	purgeDir := t.TempDir()
	published := time.Now()

	if err := GenerateMockFolder(installedDir, "Test", "1.0.0", published); err != nil {
		t.Fatal(err)
	}
	if err := GenerateMockFolder(updateDir, "Test", "1.0.0", published); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installedDir, "portmaster"), []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}

	updater, err := New(stub, "Test", Config{
		Name:              "Test",
		Directory:         installedDir,
		DownloadDirectory: updateDir,
		PurgeDirectory:    purgeDir,
		IndexFile:         "index.json",
		AutoDownload:      true,
		AutoApply:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updater.corruptedInstallation == nil {
		t.Fatal("expected corrupted installation to be detected")
	}

	m := mgr.New("updates repair test")
	if err := m.Do("repair corrupted resources", func(w *mgr.WorkerCtx) error {
		return updater.updateAndUpgrade(w, nil, false, false)
	}); err != nil {
		t.Fatal(err)
	}

	if updater.corruptedInstallation != nil {
		t.Fatalf("expected corruption state to be cleared, got %v", updater.corruptedInstallation)
	}
	if err := updater.index.VerifyArtifacts(installedDir); err != nil {
		t.Fatalf("expected installed artifacts to be repaired: %v", err)
	}
}

// GenerateMockFolder generates mock index folder for testing.
func GenerateMockFolder(dir, name, version string, published time.Time) error {
	// Make sure dir exists
	_ = os.MkdirAll(dir, 0o750)

	// Each file gets unique content so their SHA256 hashes differ.
	// gatherExistingFiles keys files by hash, so identical content would
	// cause all but one file to be lost from the lookup map.
	mockFiles := map[string][]byte{
		"portmaster":      []byte("mock:portmaster"),
		"portmaster-core": []byte("mock:portmaster-core"),
		"portmaster.zip":  []byte("mock:portmaster.zip"),
		"assets.zip":      []byte("mock:assets.zip"),
	}
	for filename, content := range mockFiles {
		if err := os.WriteFile(filepath.Join(dir, filename), content, 0o600); err != nil {
			return err
		}
	}

	index, err := GenerateIndexFromDir(dir, IndexScanConfig{
		Name:    name,
		Version: version,
	})
	if err != nil {
		return err
	}
	index.Published = published

	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal index: %s\n", err)
	}

	err = os.WriteFile(filepath.Join(dir, "index.json"), indexJSON, 0o600)
	if err != nil {
		return err
	}
	return nil
}

func TestFailedPostUpgradeCommandRestoresIndex(t *testing.T) {
	t.Parallel()

	stub := &testInstance{}
	installedDir := t.TempDir()
	updateDir := t.TempDir()
	purgeDir := t.TempDir()
	now := time.Now()

	err := GenerateMockFolder(installedDir, "Test", "1.0.0", now)
	if err != nil {
		t.Fatal(err)
	}
	err = GenerateMockFolder(updateDir, "Test", "1.0.1", now.Add(1*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	updater, err := New(stub, "Test", Config{
		Name:              "Test",
		Directory:         installedDir,
		DownloadDirectory: updateDir,
		PurgeDirectory:    purgeDir,
		IndexFile:         "index.json",
		AutoDownload:      true,
		AutoApply:         true,
		PostUpgradeCommands: []UpdateCommandConfig{{
			Command:     "false",
			FailOnError: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The upgrade must fail because of the post-upgrade command and be rolled back.
	m := mgr.New("updates rollback test")
	err = m.Do("failing update and upgrade", func(w *mgr.WorkerCtx) error {
		return updater.updateAndUpgrade(w, nil, false, false)
	})
	if err == nil {
		t.Fatal("expected upgrade to fail")
	}

	// The files on disk have been rolled back to the old version.
	diskIndex, err := LoadIndex(filepath.Join(installedDir, "index.json"), updater.cfg.Platform, nil)
	if err != nil {
		t.Fatal(err)
	}
	if diskIndex.Version != "1.0.0" {
		t.Fatalf("expected rolled back index v1.0.0 on disk, found v%s", diskIndex.Version)
	}

	// The active index must match the rolled back files.
	index, err := updater.GetIndex()
	if err != nil {
		t.Fatal(err)
	}
	if index.Version != diskIndex.Version {
		t.Fatalf("active index is v%s, but installed files were rolled back to v%s", index.Version, diskIndex.Version)
	}
}
