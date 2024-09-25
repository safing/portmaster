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

	// ResourceUpdateEvent is emitted every time the
	// updater successfully performed a resource update.
	// ResourceUpdateEvent is emitted even if no new
	// versions are available. Subscribers are expected
	// to check if new versions of their resources are
	// available by checking File.UpgradeAvailable().
	ResourceUpdateEvent = "resource update"

	// VersionUpdateEvent is emitted every time a new
	// version of a monitored resource is selected.
	// During module initialization VersionUpdateEvent
	// is also emitted.
	VersionUpdateEvent = "active version update"
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
	EventVersionsUpdated  *mgr.EventMgr[struct{}]

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
		EventVersionsUpdated:  mgr.NewEventMgr[struct{}](VersionUpdateEvent, m),

		autoApply:    index.AutoApply,
		needsRestart: index.NeedsRestart,

		instance: instance,
	}

	// Events
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.checkForUpdates, nil)
	module.updateCheckWorkerMgr.Repeat(updateTaskRepeatDuration)
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
	err := u.downloader.downloadIndexFile(wc.Ctx())
	if err != nil {
		return fmt.Errorf("failed to download index file: %w", err)
	}

	defer u.EventResourcesUpdated.Submit(struct{}{})

	if u.downloader.version.LessThanOrEqual(u.registry.version) {
		log.Infof("updates: check compete: no new updates")
		return nil
	}
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
			u.upgraderWorkerMgr.Go()
		} else {
			notifications.NotifyPrompt(updateAvailableNotificationID, "Update available", "Apply update and restart?", notifications.Action{
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
	log.Infof("update: starting update: %s %s -> %s", currentBundle.Name, currentBundle.Version, downloadBundle.Version)
	err := u.registry.performRecoverableUpgrade(u.downloader.dir, u.downloader.indexFile)
	if err != nil {
		// TODO(vladimir): Send notification to UI
		log.Errorf("updates: failed to apply updates: %s", err)
	} else if u.needsRestart {
		// TODO(vladimir): Prompt user to restart?
		u.instance.Restart()
	}
	u.EventResourcesUpdated.Submit(struct{}{})
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
