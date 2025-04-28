package ui

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

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

	archives     map[string]*zipfs.FileSystem
	archivesLock sync.RWMutex

	upgradeLock atomic.Bool
}

// New returns a new UI module.
func New(instance instance) (*UI, error) {
	m := mgr.New("UI")
	ui := &UI{
		mgr:      m,
		instance: instance,

		archives: make(map[string]*zipfs.FileSystem),
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

func (ui *UI) getArchive(name string) (archive *zipfs.FileSystem, ok bool) {
	ui.archivesLock.RLock()
	defer ui.archivesLock.RUnlock()

	archive, ok = ui.archives[name]
	return
}

func (ui *UI) setArchive(name string, archive *zipfs.FileSystem) {
	ui.archivesLock.Lock()
	defer ui.archivesLock.Unlock()

	ui.archives[name] = archive
}

// CloseArchives closes all open archives.
func (ui *UI) CloseArchives() {
	if ui == nil {
		return
	}

	ui.archivesLock.Lock()
	defer ui.archivesLock.Unlock()

	// Close archives.
	for _, archive := range ui.archives {
		if err := archive.Close(); err != nil {
			ui.mgr.Warn("failed to close ui archive", "err", err)
		}
	}

	// Reset map.
	clear(ui.archives)
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
