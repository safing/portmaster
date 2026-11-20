package process

import (
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

type ProcessModule struct {
	mgr      *mgr.Manager
	instance instance

	// uiPathLock guards uiPath and uiPathChecked.
	uiPathLock    sync.Mutex
	uiPath        string
	uiPathChecked time.Time
}

func (pm *ProcessModule) Manager() *mgr.Manager {
	return pm.mgr
}

func (pm *ProcessModule) Start() error {
	if pm.portmasterUIPath() == "" {
		// Installations without the UI, like servers, are fine.
		log.Infof("process: the Portmaster UI is not installed (yet)")
	}
	return nil
}

func (pm *ProcessModule) Stop() error {
	return nil
}

// uiPathRecheckInterval is how often the path of the UI is looked up again
// while the UI is not installed.
const uiPathRecheckInterval = 10 * time.Second

// portmasterUIPath returns the real path of the Portmaster UI binary, or an
// empty string if it is not installed. The path is looked up again
// later if the UI is missing, as the updater may install it after start.
func (pm *ProcessModule) portmasterUIPath() string {
	pm.uiPathLock.Lock()
	defer pm.uiPathLock.Unlock()

	if pm.uiPath != "" || time.Since(pm.uiPathChecked) < uiPathRecheckInterval {
		return pm.uiPath
	}
	pm.uiPathChecked = time.Now()

	identifier := "portmaster"
	if runtime.GOOS == "windows" {
		identifier += ".exe"
	}
	file, err := pm.instance.BinaryUpdates().GetFile(identifier)
	if err != nil {
		log.Debugf("process: failed to get path of ui: %s", err)
		return ""
	}

	// Processes are compared by their real path.
	path := file.Path()
	realPath, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = realPath
	}
	pm.uiPath = path
	return path
}

func prep() error {
	if err := registerConfiguration(); err != nil {
		return err
	}

	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

var (
	module     *ProcessModule
	shimLoaded atomic.Bool
)

// New returns a new Process module.
func New(instance instance) (*ProcessModule, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("ProcessModule")
	module = &ProcessModule{
		mgr:      m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}
	return module, nil
}

type instance interface {
	BinaryUpdates() *updates.Updater
}
