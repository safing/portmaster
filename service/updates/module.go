package updates

import (
	"errors"
	"fmt"
	"os"
	"time"

	semver "github.com/hashicorp/go-version"
	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

type File struct {
	id   string
	path string
}

func (f *File) Identifier() string {
	return f.id
}

func (f *File) Path() string {
	return f.path
}

func (f *File) Version() string {
	return ""
}

var ErrNotFound error = errors.New("file not found")

// Updates provides access to released artifacts.
type Updates struct {
	m      *mgr.Manager
	states *mgr.StateMgr

	updateCheckWorkerMgr *mgr.WorkerMgr
	upgraderWorkerMgr    *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]
	EventVersionsUpdated  *mgr.EventMgr[struct{}]

	updateIndex UpdateIndex

	bundle       *Bundle
	updateBundle *Bundle

	files map[string]File

	instance instance
}

// New returns a new Updates module.
func New(instance instance, name string, index UpdateIndex) (*Updates, error) {
	m := mgr.New(name)
	module := &Updates{
		m:      m,
		states: m.NewStateMgr(),

		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),

		updateIndex: index,

		instance: instance,
	}

	// Events
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.checkForUpdates, nil)
	module.updateCheckWorkerMgr.Repeat(30 * time.Second)
	module.upgraderWorkerMgr = m.NewWorkerMgr("upgrader", func(w *mgr.WorkerCtx) error {
		err := applyUpdates(module.updateIndex, *module.updateBundle)
		if err != nil {
			// TODO(vladimir): Send notification to UI
			log.Errorf("updates: failed to apply updates: %s", err)
		} else {
			// TODO(vladimir): Prompt user to restart?
			module.instance.Restart()
		}
		return nil
	}, nil)

	var err error
	module.bundle, err = ParseBundle(module.updateIndex.Directory, module.updateIndex.IndexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse binary bundle: %s", err)
	}

	// Add bundle artifacts to registry.
	module.processBundle(module.bundle)

	// Remove old files
	m.Go("old files cleaner", func(ctx *mgr.WorkerCtx) error {
		err := os.RemoveAll(module.updateIndex.PurgeDirectory)
		if err != nil {
			return fmt.Errorf("failed to delete folder: %w", err)
		}
		return nil
	})
	return module, nil
}

func (reg *Updates) processBundle(bundle *Bundle) {
	for _, artifact := range bundle.Artifacts {
		artifactPath := fmt.Sprintf("%s/%s", reg.updateIndex.Directory, artifact.Filename)
		reg.files[artifact.Filename] = File{id: artifact.Filename, path: artifactPath}
	}
}

func (u *Updates) checkForUpdates(_ *mgr.WorkerCtx) error {
	err := u.updateIndex.DownloadIndexFile()
	if err != nil {
		return fmt.Errorf("failed to download index file: %s", err)
	}

	u.updateBundle, err = ParseBundle(u.updateIndex.DownloadDirectory, u.updateIndex.IndexFile)
	if err != nil {
		return fmt.Errorf("failed parse bundle: %s", err)
	}
	defer u.EventResourcesUpdated.Submit(struct{}{})

	// Compare current and downloaded index version.
	currentVersion, err := semver.NewVersion(u.bundle.Version)
	downloadVersion, err := semver.NewVersion(u.updateBundle.Version)
	if currentVersion.Compare(downloadVersion) <= 0 {
		// no updates
		log.Info("updates: check complete: no new updates")
		return nil
	}

	log.Infof("updates: check complete: downloading new version: %s %s", u.updateBundle.Name, u.updateBundle.Version)
	err = u.DownloadUpdates()
	if err != nil {
		log.Errorf("updates: failed to download bundle: %s", err)
	} else if u.updateIndex.AutoApply {
		u.ApplyUpdates()
	}
	return nil
}

// DownloadUpdates downloads available binary updates.
func (u *Updates) DownloadUpdates() error {
	if u.updateBundle == nil {
		// CheckForBinaryUpdates needs to be called before this.
		return fmt.Errorf("no valid update bundle found")
	}
	_ = deleteUnfinishedDownloads(u.updateIndex.DownloadDirectory)
	err := u.updateBundle.CopyMatchingFilesFromCurrent(*u.bundle, u.updateIndex.Directory, u.updateIndex.DownloadDirectory)
	if err != nil {
		log.Warningf("updates: error while coping file from current to update: %s", err)
	}
	u.updateBundle.DownloadAndVerify(u.updateIndex.DownloadDirectory)
	return nil
}

func (u *Updates) ApplyUpdates() {
	u.upgraderWorkerMgr.Go()
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
	u.updateCheckWorkerMgr.Go()
	return nil
}

func (u *Updates) GetFile(id string) (*File, error) {
	file, ok := u.files[id]
	if ok {
		return &file, nil
	} else {
		log.Errorf("updates: requested file id not found: %s", id)
		for _, file := range u.files {
			log.Debugf("File: %s", file)
		}
		return nil, ErrNotFound
	}
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
