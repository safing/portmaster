package ui

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

func prep() error {
	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return registerRoutes()
}

func start() error {
	// Create a dummy directory to which processes change their working directory
	// to. Currently this includes the App and the Notifier. The aim is protect
	// all other directories and increase compatibility should any process want
	// to read or write something to the current working directory. This can also
	// be useful in the future to dump data to for debugging. The permission used
	// may seem dangerous, but proper permission on the parent directory provide
	// (some) protection.
	// Processes must _never_ read from this directory.
	execDir := filepath.Join(module.instance.DataDir(), "exec")
	err := os.MkdirAll(execDir, 0o0777) //nolint:gosec // This is intentional.
	if err != nil {
		log.Warningf("ui: failed to create safe exec dir: %s", err)
	}

	// Ensure directory permission
	err = utils.EnsureDirectory(execDir, utils.PublicWritePermission)
	if err != nil {
		log.Warningf("ui: failed to set permissions to directory %s: %s", execDir, err)
	}

	return nil
}

// UI serves the user interface files.
type UI struct {
	mgr *mgr.Manager

	instance instance
}

func (ui *UI) Manager() *mgr.Manager {
	return ui.mgr
}

// Start starts the module.
func (ui *UI) Start() error {
	return start()
}

// Stop stops the module.
func (ui *UI) Stop() error {
	return nil
}

var (
	shimLoaded atomic.Bool
	module     *UI
)

// New returns a new UI module.
func New(instance instance) (*UI, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("UI")
	module = &UI{
		mgr:      m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	DataDir() string
	API() *api.API
	BinaryUpdates() *updates.Updater
}
