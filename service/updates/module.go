package updates

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	semver "github.com/hashicorp/go-version"
	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

const updateAvailableNotificationID = "updates:update-available"

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
		files:       make(map[string]File),

		instance: instance,
	}

	// Events
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.checkForUpdates, nil)
	module.updateCheckWorkerMgr.Repeat(1 * time.Hour)
	module.upgraderWorkerMgr = m.NewWorkerMgr("upgrader", module.applyUpdates, nil)

	var err error
	module.bundle, err = ParseBundle(module.updateIndex.Directory, module.updateIndex.IndexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse binary bundle: %s", err)
	}

	// Add bundle artifacts to registry.
	module.processBundle(module.bundle)
	err = module.registerEndpoints()
	if err != nil {
		log.Errorf("failed to register endpoints: %s", err)
	}

	return module, nil
}

func (u *Updates) registerEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Check for update",
		Description: "Trigger update check",
		Path:        "updates/check",
		Read:        api.PermitAnyone,
		ActionFunc: func(ar *api.Request) (msg string, err error) {
			u.updateCheckWorkerMgr.Go()
			return "Check for updates triggered", nil
		},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Apply update",
		Description: "Triggers update",
		Path:        "updates/apply",
		Read:        api.PermitAnyone,
		ActionFunc: func(ar *api.Request) (msg string, err error) {
			u.upgraderWorkerMgr.Go()
			return "Apply updates triggered", nil
		},
	}); err != nil {
		return err
	}

	return nil
}

func (reg *Updates) processBundle(bundle *Bundle) {
	for _, artifact := range bundle.Artifacts {
		artifactPath := fmt.Sprintf("%s/%s", reg.updateIndex.Directory, artifact.Filename)
		reg.files[artifact.Filename] = File{id: artifact.Filename, path: artifactPath}
	}
}

func (u *Updates) checkForUpdates(_ *mgr.WorkerCtx) error {
	httpClient := http.Client{}
	err := u.updateIndex.DownloadIndexFile(&httpClient)
	if err != nil {
		return fmt.Errorf("failed to download index file: %s", err)
	}

	u.updateBundle, err = ParseBundle(u.updateIndex.DownloadDirectory, u.updateIndex.IndexFile)
	if err != nil {
		return fmt.Errorf("failed parsing bundle: %s", err)
	}
	defer u.EventResourcesUpdated.Submit(struct{}{})

	hasUpdate, err := u.checkVersionIncrement()
	if err != nil {
		return fmt.Errorf("failed to compare versions: %s", err)
	}

	if !hasUpdate {
		log.Infof("updates: check compete: no new updates")
		return nil
	}

	log.Infof("updates: check complete: downloading new version: %s %s", u.updateBundle.Name, u.updateBundle.Version)
	err = u.downloadUpdates(&httpClient)
	if err != nil {
		log.Errorf("updates: failed to download bundle: %s", err)
	} else {
		notifications.NotifyPrompt(updateAvailableNotificationID, "Update available", "Apply update and restart?", notifications.Action{
			ID:      "apply",
			Text:    "Apply",
			Type:    notifications.ActionTypeInjectEvent,
			Payload: "apply-updates",
		})
	}
	return nil
}

func (u *Updates) checkVersionIncrement() (bool, error) {
	// Compare current and downloaded index version.
	currentVersion, err := semver.NewVersion(u.bundle.Version)
	if err != nil {
		return false, err
	}
	downloadVersion, err := semver.NewVersion(u.updateBundle.Version)
	if err != nil {
		return false, err
	}
	log.Debugf("updates: checking version: curr: %s new: %s", currentVersion.String(), downloadVersion.String())
	return downloadVersion.GreaterThan(currentVersion), nil
}

func (u *Updates) downloadUpdates(client *http.Client) error {
	if u.updateBundle == nil {
		// checkForUpdates needs to be called before this.
		return fmt.Errorf("no valid update bundle found")
	}
	_ = deleteUnfinishedDownloads(u.updateIndex.DownloadDirectory)
	err := u.updateBundle.CopyMatchingFilesFromCurrent(*u.bundle, u.updateIndex.Directory, u.updateIndex.DownloadDirectory)
	if err != nil {
		log.Warningf("updates: error while coping file from current to update: %s", err)
	}
	u.updateBundle.DownloadAndVerify(client, u.updateIndex.DownloadDirectory)
	return nil
}

func (u *Updates) applyUpdates(_ *mgr.WorkerCtx) error {
	// Check if we have new version
	hasNewVersion, err := u.checkVersionIncrement()
	if err != nil {
		return fmt.Errorf("error while reading bundle version: %w", err)
	}

	if !hasNewVersion {
		return fmt.Errorf("there is no new version to apply")
	}

	err = u.updateBundle.Verify(u.updateIndex.DownloadDirectory)
	if err != nil {
		return fmt.Errorf("failed to apply update: %s", err)
	}

	err = switchFolders(u.updateIndex, *u.updateBundle)
	if err != nil {
		// TODO(vladimir): Send notification to UI
		log.Errorf("updates: failed to apply updates: %s", err)
	} else {
		// TODO(vladimir): Prompt user to restart?
		u.instance.Restart()
	}
	return nil
}

// TriggerUpdateCheck triggers an update check
func (u *Updates) TriggerUpdateCheck() {
	u.updateCheckWorkerMgr.Go()
}

// TriggerApplyUpdates triggers upgrade
func (u *Updates) TriggerApplyUpdates() {
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
	// Remove old files
	u.m.Go("old files cleaner", func(ctx *mgr.WorkerCtx) error {
		err := os.RemoveAll(u.updateIndex.PurgeDirectory)
		if err != nil {
			return fmt.Errorf("failed to delete folder: %w", err)
		}
		return nil
	})
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
