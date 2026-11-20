package updates

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/service/mgr"
)

// newRestartTestUpdater returns an updater with version 1.0.0 installed and
// version 1.0.1 ready in the download directory, which needs a restart.
func newRestartTestUpdater(t *testing.T) (*Updater, string) {
	t.Helper()

	installedDir := t.TempDir()
	updateDir := t.TempDir()
	now := time.Now()
	require.NoError(t, GenerateMockFolder(installedDir, rollbackTestName, "1.0.0", now))
	require.NoError(t, GenerateMockFolder(updateDir, rollbackTestName, "1.0.1", now.Add(time.Minute)))

	updater, err := New(&testInstance{}, rollbackTestName, Config{
		Name:              rollbackTestName,
		Directory:         installedDir,
		DownloadDirectory: updateDir,
		PurgeDirectory:    t.TempDir(),
		IndexFile:         rollbackTestIndex,
		AutoDownload:      true,
		AutoApply:         true,
		NeedsRestart:      true,
		Notify:            true,
	})
	require.NoError(t, err)
	return updater, installedDir
}

func TestUpgradeNeedingRestartReturnsErrRestartRequired(t *testing.T) {
	t.Parallel()

	updater, _ := newRestartTestUpdater(t)
	err := updater.m.Do("test", func(w *mgr.WorkerCtx) error {
		return updater.updateAndUpgrade(w, nil, true, true)
	})
	require.ErrorIs(t, err, ErrRestartRequired)
	require.ErrorIs(t, err, ErrActionRequired)
}

func TestForceUpdateNeedingRestartSucceeds(t *testing.T) {
	t.Parallel()

	updater, installedDir := newRestartTestUpdater(t)
	require.NoError(t, updater.ForceUpdate())

	index, err := LoadIndex(filepath.Join(installedDir, rollbackTestIndex), updater.cfg.Platform, nil)
	require.NoError(t, err)
	require.Equal(t, "1.0.1", index.Version)
}
