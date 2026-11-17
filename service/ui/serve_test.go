package ui

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
		archives: make(map[string]*archive),
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
				// Let the upgrade lock loop run, also with GOMAXPROCS=1.
				runtime.Gosched()
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

// writeTestZip writes a zip archive with the given entries to a new file.
// Entry names ending in "/" are directory entries.
func writeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()

	zipPath := filepath.Join(t.TempDir(), "portmaster.zip")
	f, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	return zipPath
}

func serveTestPath(ui *UI, resPath string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/ui/modules/portmaster/"+resPath, nil)
	r = r.WithContext(context.WithValue(r.Context(), api.RequestContextKey, &api.Request{
		Request: r,
		URLVars: map[string]string{"moduleName": "portmaster", "resPath": resPath},
	}))
	w := httptest.NewRecorder()
	(&archiveServer{ui: ui}).ServeHTTP(w, r)
	return w
}

// TestServeDirectoryEntry checks that a path naming a directory in the archive
// is handled like a non-existent path instead of returning an empty 200.
func TestServeDirectoryEntry(t *testing.T) {
	t.Parallel()

	t.Run("fallback to index", func(t *testing.T) {
		t.Parallel()

		ui, ti := newTestUI(t)
		ti.zipPath = writeTestZip(t, map[string]string{
			"index.html":    "<html>index</html>",
			"assets/":       "",
			"assets/app.js": "console.log(1)",
		})

		missing := serveTestPath(ui, "does-not-exist")
		require.Equal(t, http.StatusOK, missing.Code)

		w := serveTestPath(ui, "assets")
		assert.Equal(t, missing.Code, w.Code)
		assert.Equal(t, "<html>index</html>", w.Body.String())
	})

	t.Run("not found without index", func(t *testing.T) {
		t.Parallel()

		ui, ti := newTestUI(t)
		ti.zipPath = writeTestZip(t, map[string]string{
			"assets/":       "",
			"assets/app.js": "console.log(1)",
		})

		missing := serveTestPath(ui, "does-not-exist")
		require.Equal(t, http.StatusNotFound, missing.Code)

		w := serveTestPath(ui, "assets")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestCloseArchivesWaitsForServedFiles checks that an archive is only closed
// once the files served from it are closed, and that new requests are not
// blocked meanwhile.
func TestCloseArchivesWaitsForServedFiles(t *testing.T) { //nolint:paralleltest // Modifies archiveCloseWait.
	ui, _ := newTestUI(t)

	// Open a file and keep it open, as a slow request would.
	file, _, _, err := ui.openArchiveFile("portmaster", indexFile)
	require.NoError(t, err)

	closed := make(chan struct{})
	go func() {
		ui.CloseArchives()
		close(closed)
	}()

	// New requests are served while the old archive waits to be closed.
	served := make(chan int, 1)
	go func() { served <- serveTestRequest(ui).Code }()
	select {
	case code := <-served:
		assert.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("request blocked while an archive waits to be closed")
	}

	select {
	case <-closed:
		t.Fatal("archive closed while a file served from it is still open")
	case <-time.After(100 * time.Millisecond):
	}

	// The open file can still be read.
	buf := make([]byte, 6)
	_, err = file.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "<html>", string(buf))

	require.NoError(t, file.Close())
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("archive not closed after the served file was closed")
	}
}

// TestCloseArchivesGivesUpWaiting checks that CloseArchives does not wait
// forever for a file that is not closed, and closes the archive when it is.
func TestCloseArchivesGivesUpWaiting(t *testing.T) { //nolint:paralleltest // Modifies archiveCloseWait.
	oldWait := archiveCloseWait
	archiveCloseWait = 50 * time.Millisecond
	t.Cleanup(func() { archiveCloseWait = oldWait })

	ui, _ := newTestUI(t)
	ui.archivesLock.RLock()
	require.Empty(t, ui.archives)
	ui.archivesLock.RUnlock()

	file, _, _, err := ui.openArchiveFile("portmaster", indexFile)
	require.NoError(t, err)
	ui.archivesLock.RLock()
	a := ui.archives["portmaster"]
	ui.archivesLock.RUnlock()
	require.NotNil(t, a)

	closed := make(chan struct{})
	go func() {
		ui.CloseArchives()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("CloseArchives did not give up waiting for an open file")
	}

	// The archive is closed in the background once the file is closed.
	require.NoError(t, file.Close())
	released := make(chan struct{})
	go func() {
		a.inUse.Wait()
		close(released)
	}()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("archive still in use after the file was closed")
	}

	// Closing a served file twice releases the archive only once; releasing
	// it twice would panic.
	assert.NotPanics(t, func() { _ = file.Close() })
}
