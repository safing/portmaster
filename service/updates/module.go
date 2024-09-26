package updates

import (
	"fmt"
	"runtime"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

const (
	updateTaskRepeatDuration      = 1 * time.Hour
	updateAvailableNotificationID = "updates:update-available"
	updateFailedNotificationID    = "updates:update-failed"

	// ResourceUpdateEvent is emitted every time the
	// updater successfully performed a resource update.
	ResourceUpdateEvent = "resource update"
)

// UserAgent is an HTTP User-Agent that is used to add
// more context to requests made by the registry when
// fetching resources from the update server.
var UserAgent = fmt.Sprintf("Portmaster (%s %s)", runtime.GOOS, runtime.GOARCH)

// UpdateIndex holds the configuration for the updates module.
type UpdateIndex struct {
	Directory         string
	DownloadDirectory string
	PurgeDirectory    string
	Ignore            []string
	IndexURLs         []string
	IndexFile         string
	AutoApply         bool
	NeedsRestart      bool
}

// Updates provides access to released artifacts.
type Updates struct {
	m      *mgr.Manager
	states *mgr.StateMgr

	updateCheckWorkerMgr *mgr.WorkerMgr
	upgraderWorkerMgr    *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]

	registry   Registry
	downloader Downloader

	autoApply    bool
	needsRestart bool

	instance instance
}

// New returns a new Updates module.
func New(instance instance, name string, index UpdateIndex) (*Updates, error) {
	m := mgr.New(name)
	module := &Updates{
		m:      m,
		states: m.NewStateMgr(),

		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),

		autoApply:    index.AutoApply,
		needsRestart: index.NeedsRestart,

		instance: instance,
	}

	// Workers
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.checkForUpdates, nil).Repeat(updateTaskRepeatDuration)
	module.upgraderWorkerMgr = m.NewWorkerMgr("upgrader", module.applyUpdates, nil)

	var err error
	module.registry, err = CreateRegistry(index)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry: %w", err)
	}

	module.downloader = CreateDownloader(index)

	return module, nil
}

func (u *Updates) checkForUpdates(wc *mgr.WorkerCtx) error {
	// Download the index file.
	err := u.downloader.downloadIndexFile(wc.Ctx())
	if err != nil {
		return fmt.Errorf("failed to download index file: %w", err)
	}
	// Check if there is a new version.
	if u.downloader.version.LessThanOrEqual(u.registry.version) {
		log.Infof("updates: check compete: no new updates")
		return nil
	}

	// Download the new version.
	downloadBundle := u.downloader.bundle
	log.Infof("updates: check complete: downloading new version: %s %s", downloadBundle.Name, downloadBundle.Version)
	err = u.downloader.copyMatchingFilesFromCurrent(u.registry.files)
	if err != nil {
		log.Warningf("updates: failed to copy files from current installation: %s", err)
	}
	err = u.downloader.downloadAndVerify(wc.Ctx())
	if err != nil {
		log.Errorf("updates: failed to download update: %s", err)
	} else {
		if u.autoApply {
			// Trigger upgrade.
			u.upgraderWorkerMgr.Go()
		} else {
			// Notify the user with option to trigger upgrade.
			notifications.NotifyPrompt(updateAvailableNotificationID, "New update is available.", fmt.Sprintf("%s %s", downloadBundle.Name, downloadBundle.Version), notifications.Action{
				ID:   "apply",
				Text: "Apply",
				Type: notifications.ActionTypeWebhook,
				Payload: notifications.ActionTypeWebhookPayload{
					Method: "POST",
					URL:    "updates/apply",
				},
			})
		}
	}
	return nil
}

func (u *Updates) applyUpdates(_ *mgr.WorkerCtx) error {
	currentBundle := u.registry.bundle
	downloadBundle := u.downloader.bundle
	if u.downloader.version.LessThanOrEqual(u.registry.version) {
		// No new version, silently return.
		return nil
	}

	log.Infof("update: starting update: %s %s -> %s", currentBundle.Name, currentBundle.Version, downloadBundle.Version)
	err := u.registry.performRecoverableUpgrade(u.downloader.dir, u.downloader.indexFile)
	if err != nil {
		// Notify the user that update failed.
		notifications.NotifyPrompt(updateFailedNotificationID, "Failed to apply update.", err.Error())
		return fmt.Errorf("updates: failed to apply updates: %w", err)
	}

	if u.needsRestart {
		// Perform restart.
		u.instance.Restart()
	} else {
		// Update completed and no restart is needed. Submit an event.
		u.EventResourcesUpdated.Submit(struct{}{})
	}
	return nil
}

// TriggerUpdateCheck triggers an update check.
func (u *Updates) TriggerUpdateCheck() {
	u.updateCheckWorkerMgr.Go()
}

// TriggerApplyUpdates triggers upgrade.
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
		_ = u.registry.CleanOldFiles()
		_ = u.downloader.deleteUnfinishedDownloads()
		return nil
	})
	u.updateCheckWorkerMgr.Go()

	return nil
}

func (u *Updates) GetRootPath() string {
	return u.registry.dir
}

// GetFile returns the path of a file given the name.
func (u *Updates) GetFile(id string) (*File, error) {
	file, ok := u.registry.files[id]
	if ok {
		return &file, nil
	} else {
		log.Errorf("updates: requested file id not found: %s", id)
		return nil, ErrNotFound
	}
}

// Stop stops the module.
func (u *Updates) Stop() error {
	return nil
}

type instance interface {
	API() *api.API
	Config() *config.Config
	Restart()
	Shutdown()
	Notifications() *notifications.Notifications
}
