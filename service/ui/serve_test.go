package ui

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spkg/zipfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/service/mgr"
)

type testInstance struct {
	zipPath string
	calls   atomic.Int32
	hook    func()
}

func (ti *testInstance) DataDir() string { return "" }

func (ti *testInstance) API() *api.API { return nil }

func (ti *testInstance) GetBinaryUpdateFile(_ string) (string, error) {
	ti.calls.Add(1)
	if ti.hook != nil {
		ti.hook()
	}
	return ti.zipPath, nil
}

func newTestUI(t *testing.T) (*UI, *testInstance) {
	t.Helper()

	zipPath := filepath.Join(t.TempDir(), "portmaster.zip")
	f, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create(indexFile)
	require.NoError(t, err)
	_, err = w.Write([]byte(strings.Repeat("<html></html>\n", 1000)))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	ti := &testInstance{zipPath: zipPath}
	ui := &UI{
		mgr:      mgr.New("UI"),
		instance: ti,
		archives: make(map[string]*zipfs.FileSystem),
	}
	t.Cleanup(ui.CloseArchives)
	return ui, ti
}

func serveTestRequest(ui *UI) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/ui/modules/portmaster/index.html", nil)
	r = r.WithContext(context.WithValue(r.Context(), api.RequestContextKey, &api.Request{
		Request: r,
		URLVars: map[string]string{"moduleName": "portmaster", "resPath": indexFile},
	}))
	w := httptest.NewRecorder()
	(&archiveServer{ui: ui}).ServeHTTP(w, r)
	return w
}

// countOpenFDs returns how many file descriptors of this process point to path.
func countOpenFDs(t *testing.T, path string) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("cannot inspect open file descriptors:", err)
	}
	var n int
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name()))
		if err == nil && target == path {
			n++
		}
	}
	return n
}

// TestServeConcurrentWithUpgradeLock checks that serving from a cached archive
// does not race with the archives being closed by the upgrade lock.
func TestServeConcurrentWithUpgradeLock(t *testing.T) {
	t.Parallel()

	ui, _ := newTestUI(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				serveTestRequest(ui)
			}
		})
	}
	for range 200 {
		ui.EnableUpgradeLock()
		ui.DisableUpgradeLock()
		time.Sleep(100 * time.Microsecond)
	}
	close(stop)
	wg.Wait()
}

// TestNoArchiveCachedDuringUpgrade checks that a request that started before
// the upgrade lock was enabled does not leave an archive (and its open file)
// cached while the upgrade is in progress.
func TestNoArchiveCachedDuringUpgrade(t *testing.T) {
	t.Parallel()

	ui, ti := newTestUI(t)

	// Simulate the upgrade starting right after the request checked the
	// upgrade lock, but before it opened the archive.
	// Note: The updater holds its index lock (which GetBinaryUpdateFile needs)
	// while calling EnableUpgradeLock, so this must not deadlock either.
	ti.hook = ui.EnableUpgradeLock

	w := serveTestRequest(ui)
	assert.Equal(t, http.StatusLocked, w.Code)

	ui.archivesLock.RLock()
	cached := len(ui.archives)
	ui.archivesLock.RUnlock()
	assert.Equal(t, 0, cached, "no archive may be cached while the upgrade lock is enabled")
	assert.Equal(t, 0, countOpenFDs(t, ti.zipPath), "archive file must not be kept open during the upgrade")

	// After the upgrade, the archive is loaded again.
	ti.hook = nil
	ui.DisableUpgradeLock()
	w = serveTestRequest(ui)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestConcurrentArchiveLoadDoesNotLeak checks that concurrent first requests
// for the same module do not leak an opened archive.
func TestConcurrentArchiveLoadDoesNotLeak(t *testing.T) {
	t.Parallel()

	ui, ti := newTestUI(t)

	// Let both requests load the archive at the same time.
	ti.hook = func() {
		deadline := time.Now().Add(200 * time.Millisecond)
		for ti.calls.Load() < 2 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			w := serveTestRequest(ui)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
	wg.Wait()

	ui.CloseArchives()
	assert.Equal(t, 0, countOpenFDs(t, ti.zipPath), "all opened archives must be closed by CloseArchives")
}
