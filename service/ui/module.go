package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
	"github.com/spkg/zipfs"
)

// UI serves the user interface files.
type UI struct {
	mgr      *mgr.Manager
	instance instance

	archives     map[string]*archive
	archivesLock sync.RWMutex

	upgradeLock atomic.Bool
}

// New returns a new UI module.
func New(instance instance) (*UI, error) {
	m := mgr.New("UI")
	ui := &UI{
		mgr:      m,
		instance: instance,

		archives: make(map[string]*archive),
	}

	if err := ui.registerAPIEndpoints(); err != nil {
		return nil, err
	}
	if err := ui.registerRoutes(); err != nil {
		return nil, err
	}

	return ui, nil
}

func (ui *UI) Manager() *mgr.Manager {
	return ui.mgr
}

// Start starts the module.
func (ui *UI) Start() error {
	// Create a dummy directory to which processes change their working directory
	// to. Currently this includes the App and the Notifier. The aim is protect
	// all other directories and increase compatibility should any process want
	// to read or write something to the current working directory. This can also
	// be useful in the future to dump data to for debugging. The permission used
	// may seem dangerous, but proper permission on the parent directory provide
	// (some) protection.
	// Processes must _never_ read from this directory.
	execDir := filepath.Join(ui.instance.DataDir(), "exec")
	err := os.MkdirAll(execDir, 0o0777) //nolint:gosec // This is intentional.
	if err != nil {
		log.Warningf("ui: failed to create safe exec dir: %s", err)
	}

	// Ensure directory permission
	err = utils.EnsureDirectory(execDir, utils.PublicWriteExecPermission)
	if err != nil {
		log.Warningf("ui: failed to set permissions to directory %s: %s", execDir, err)
	}

	return nil
}

// Stop stops the module.
func (ui *UI) Stop() error {
	return nil
}

// archive is an opened UI archive with the number of files that are currently
// being served from it.
type archive struct {
	fs *zipfs.FileSystem

	// inUse counts the files opened from the archive that are not closed yet.
	// The archive must not be closed while files are read from it: on Windows,
	// closing a file while it is being read concurrently can block forever
	// (Go 1.26, golang/go#77404).
	inUse sync.WaitGroup
}

// open opens a file from the archive. The caller must hold the archives lock,
// so that the archive is not retired at the same time.
func (a *archive) open(archiveName, path string) (file http.File, servedPath string, status int, err error) {
	a.inUse.Add(1)
	file, servedPath, status, err = openFileFromArchive(archiveName, a.fs, path)
	if err != nil {
		a.inUse.Done()
		return nil, servedPath, status, err
	}
	return &archiveFile{File: file, done: a.inUse.Done}, servedPath, status, nil
}

// archiveFile is a file opened from an archive, which releases the archive
// when closed.
type archiveFile struct {
	http.File

	done     func()
	doneOnce sync.Once
}

// Close closes the file and releases the archive.
func (f *archiveFile) Close() error {
	err := f.File.Close()
	f.doneOnce.Do(f.done)
	return err
}

// archiveCloseWait is how long CloseArchives waits for files that are still
// being served before it leaves closing the archive to the background.
var archiveCloseWait = 30 * time.Second

// CloseArchives closes all open archives.
// Archives are removed from the cache immediately, but each one is only
// closed once all files served from it are closed. CloseArchives waits for
// that for a limited time and then leaves closing to the background.
func (ui *UI) CloseArchives() {
	if ui == nil {
		return
	}

	// Remove archives from the cache, so that no new files are opened from
	// them. Do not wait for them while holding the lock, so that requests
	// are not blocked.
	ui.archivesLock.Lock()
	retired := make([]*archive, 0, len(ui.archives))
	for _, a := range ui.archives {
		retired = append(retired, a)
	}
	clear(ui.archives)
	ui.archivesLock.Unlock()

	deadline := time.After(archiveCloseWait)
	for _, a := range retired {
		released := make(chan struct{})
		go func() {
			a.inUse.Wait()
			close(released)
		}()

		select {
		case <-released:
			ui.closeArchive(a)
		case <-deadline:
			ui.mgr.Warn("ui archive still in use, closing it when released")
			go func() {
				<-released
				ui.closeArchive(a)
			}()
		}
	}
}

// EnableUpgradeLock enables the upgrade lock and closes all open archives.
func (ui *UI) EnableUpgradeLock() {
	if ui == nil {
		return
	}

	ui.upgradeLock.Store(true)
	ui.CloseArchives()
}

// DisableUpgradeLock disables the upgrade lock.
func (ui *UI) DisableUpgradeLock() {
	if ui == nil {
		return
	}

	ui.upgradeLock.Store(false)
}

type instance interface {
	DataDir() string
	API() *api.API
	GetBinaryUpdateFile(name string) (path string, err error)
}

func (ui *UI) closeArchive(a *archive) {
	err := a.fs.Close()
	if err != nil {
		ui.mgr.Warn("failed to close ui archive", "err", err)
	}
}
