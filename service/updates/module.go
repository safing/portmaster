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

type UpdateIndex struct {
	Directory string
	Ignore    []string
	IndexURLs []string
	AutoApply bool
}

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

		updateWorkerMgr:       m.NewWorkerMgr("updater", checkForUpdates, nil),
		restartWorkerMgr:      m.NewWorkerMgr("automatic restart", automaticRestart, nil),
		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),
		instance:              instance,
	}

	module.binUpdates = UpdateIndex{
		Directory: "/usr/local/bin/portmaster",
		Ignore:    []string{"databases", "intel", "config.json"},
		IndexURLs: []string{"https://updates.safing.io/test-intel-1.json"},
		AutoApply: false,
	}

	module.intelUpdates = UpdateIndex{
		Directory: "/var/portmaster/intel",
		IndexURLs: []string{"https://updates.safing.io/test-stable-1.json"},
		AutoApply: true,
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

func (m *Updates) fetchUpdates() {
	binBundle, err := fetchBundle(m.binUpdates)
	if err == nil {
		// No error. Error already logged.
		log.Debugf("Bin Bundle: %+v", binBundle)
		dir := "new_bin"
		deleteUnfinishedDownloads(dir)
		downloadAndVerify(binBundle, dir)
	}
	intelBundle, err := fetchBundle(m.intelUpdates)
	if err == nil {
		// No error. Error already logged.
		log.Debugf("Intel Bundle: %+v", intelBundle)
		dir := "new_intel"
		deleteUnfinishedDownloads(dir)
		downloadAndVerify(intelBundle, dir)
	}
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
		module.fetchUpdates()
		return nil
	})
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
