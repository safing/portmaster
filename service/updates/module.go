package updates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

const (
	defaultFileMode = os.FileMode(0o0644)
	defaultDirMode  = os.FileMode(0o0755)
)

// Updates provides access to released artifacts.
type Updates struct {
	m      *mgr.Manager
	states *mgr.StateMgr

	updateWorkerMgr  *mgr.WorkerMgr
	restartWorkerMgr *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]
	EventVersionsUpdated  *mgr.EventMgr[struct{}]

	binUpdates   UpdateIndex
	intelUpdates UpdateIndex

	instance instance
}

var (
	module     *Updates
	shimLoaded atomic.Bool
)

// New returns a new UI module.
func New(instance instance) (*Updates, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("Updates")
	module = &Updates{
		m:      m,
		states: m.NewStateMgr(),

		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),
		instance:              instance,
	}

	// Events
	module.updateWorkerMgr = m.NewWorkerMgr("updater", module.checkForUpdates, nil)
	module.restartWorkerMgr = m.NewWorkerMgr("automatic restart", automaticRestart, nil)

	module.binUpdates = UpdateIndex{
		Directory:         "/usr/lib/portmaster",
		DownloadDirectory: "/var/portmaster/new_bin",
		Ignore:            []string{"databases", "intel", "config.json"},
		IndexURLs:         []string{"http://localhost:8000/test-binary.json"},
		IndexFile:         "bin-index.json",
		AutoApply:         false,
	}

	module.intelUpdates = UpdateIndex{
		Directory:         "/var/portmaster/intel",
		DownloadDirectory: "/var/portmaster/new_intel",
		IndexURLs:         []string{"http://localhost:8000/test-intel.json"},
		IndexFile:         "intel-index.json",
		AutoApply:         true,
	}

	return module, nil
}

func deleteUnfinishedDownloads(rootDir string) error {
	return filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the current file has the specified extension
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".download") {
			log.Warningf("updates deleting unfinished: %s\n", path)
			err := os.Remove(path)
			if err != nil {
				return fmt.Errorf("failed to delete file %s: %w", path, err)
			}
		}

		return nil
	})
}

func (u *Updates) checkForUpdates(_ *mgr.WorkerCtx) error {
	_ = deleteUnfinishedDownloads(u.binUpdates.DownloadDirectory)
	hasUpdate, err := u.binUpdates.checkForUpdates()
	if err != nil {
		log.Warningf("failed to get binary index file: %s", err)
	}
	if hasUpdate {
		binBundle, err := u.binUpdates.GetUpdateBundle()
		if err == nil {
			log.Debugf("Bin Bundle: %+v", binBundle)
			_ = os.MkdirAll(u.binUpdates.DownloadDirectory, defaultDirMode)
			binBundle.downloadAndVerify(u.binUpdates.DownloadDirectory)
		}
	}
	_ = deleteUnfinishedDownloads(u.intelUpdates.DownloadDirectory)
	hasUpdate, err = u.intelUpdates.checkForUpdates()
	if err != nil {
		log.Warningf("failed to get intel index file: %s", err)
	}
	if hasUpdate {
		intelBundle, err := u.intelUpdates.GetUpdateBundle()
		if err == nil {
			log.Debugf("Intel Bundle: %+v", intelBundle)
			_ = os.MkdirAll(u.intelUpdates.DownloadDirectory, defaultDirMode)
			intelBundle.downloadAndVerify(u.intelUpdates.DownloadDirectory)
		}
	}
	return nil
}

// States returns the state manager.
func (u *Updates) States() *mgr.StateMgr {
	return u.states
}

// Manager returns the module manager.
func (u *Updates) Manager() *mgr.Manager {
	return u.m
}

// Start starts the module.
func (u *Updates) Start() error {
	initConfig()
	u.m.Go("check for updates", func(w *mgr.WorkerCtx) error {
		binBundle, err := u.binUpdates.GetInstallBundle()
		if err != nil {
			log.Warningf("failed to get binary bundle: %s", err)
		} else {
			err = binBundle.Verify(u.binUpdates.Directory)
			if err != nil {
				log.Warningf("binary bundle is not valid: %s", err)
			} else {
				log.Infof("binary bundle is valid")
			}
		}

		intelBundle, err := u.intelUpdates.GetInstallBundle()
		if err != nil {
			log.Warningf("failed to get intel bundle: %s", err)
		} else {
			err = intelBundle.Verify(u.intelUpdates.Directory)
			if err != nil {
				log.Warningf("intel bundle is not valid: %s", err)
			} else {
				log.Infof("intel bundle is valid")
			}
		}

		return nil
	})
	u.updateWorkerMgr.Go()
	return nil
}

// Stop stops the module.
func (u *Updates) Stop() error {
	return stop()
}

type instance interface {
	API() *api.API
	Config() *config.Config
	Restart()
	Shutdown()
	Notifications() *notifications.Notifications
}
