package updates

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/tevino/abool"
)

const (
	updateTaskRepeatDuration          = 1 * time.Hour
	updateAvailableNotificationID     = "updates:update-available"
	updateFailedNotificationID        = "updates:update-failed"
	corruptInstallationNotificationID = "updates:corrupt-installation"

	// ResourceUpdateEvent is emitted every time the
	// updater successfully performed a resource update.
	ResourceUpdateEvent = "resource update"
)

var (
	// UserAgent is an HTTP User-Agent that is used to add
	// more context to requests made by the registry when
	// fetching resources from the update server.
	UserAgent = fmt.Sprintf("Portmaster (%s %s)", runtime.GOOS, runtime.GOARCH)

	ErrNotFound error = errors.New("file not found")
)

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
	upgradeWorkerMgr     *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]

	registry   Registry
	downloader Downloader

	autoApply    bool
	needsRestart bool

	corruptedInstallation bool

	isUpdateRunning *abool.AtomicBool

	instance instance
}

// New returns a new Updates module.
func New(instance instance, name string, index UpdateIndex) (*Updates, error) {
	m := mgr.New(name)
	module := &Updates{
		m:      m,
		states: m.NewStateMgr(),

		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),

		autoApply:       index.AutoApply,
		needsRestart:    index.NeedsRestart,
		isUpdateRunning: abool.NewBool(false),

		instance: instance,
	}

	// Workers
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.checkForUpdates, nil).Repeat(updateTaskRepeatDuration)
	module.upgradeWorkerMgr = m.NewWorkerMgr("upgrader", func(w *mgr.WorkerCtx) error {
		if !module.isUpdateRunning.SetToIf(false, true) {
			return fmt.Errorf("unable to apply updates, concurrent updater task is running")
		}
		// Make sure to unset it
		defer module.isUpdateRunning.UnSet()

		module.applyUpdates(module.downloader, false)
		return nil
	}, nil)

	var err error
	module.registry, err = CreateRegistry(index)
	if err != nil {
		// Installation is corrupt, set flag and fall back to folder scanning for artifacts discovery.
		log.Criticalf("updates: failed to create registry: %s (falling back to folder scanning)", err)
		module.corruptedInstallation = true

		module.registry, err = CreateRegistryFromFolder(index)
		if err != nil {
			return nil, err
		}
	}

	module.downloader = CreateDownloader(index)

	return module, nil
}

func (u *Updates) checkForUpdates(wc *mgr.WorkerCtx) error {
	if !u.isUpdateRunning.SetToIf(false, true) {
		return fmt.Errorf("unable to check for updates, concurrent updater task is running")
	}
	// Make sure to unset it on return.
	defer u.isUpdateRunning.UnSet()
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
			// Apply updates.
			u.applyUpdates(u.downloader, false)
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

// UpdateFromURL installs an update from the provided url.
func (u *Updates) UpdateFromURL(url string) error {
	if !u.isUpdateRunning.SetToIf(false, true) {
		return fmt.Errorf("unable to upgrade from url, concurrent updater task is running")
	}

	u.m.Go("custom-url-downloader", func(w *mgr.WorkerCtx) error {
		// Make sure to unset it on return.
		defer u.isUpdateRunning.UnSet()

		// Initialize parameters
		index := UpdateIndex{
			DownloadDirectory: u.downloader.dir,
			IndexURLs:         []string{url},
			IndexFile:         u.downloader.indexFile,
		}

		// Initialize with proper values and download the index file.
		downloader := CreateDownloader(index)
		err := downloader.downloadIndexFile(w.Ctx())
		if err != nil {
			return err
		}

		// Start downloading the artifacts
		err = downloader.downloadAndVerify(w.Ctx())
		if err != nil {
			return err
		}

		// Artifacts are downloaded, perform the update.
		u.applyUpdates(downloader, true)

		return nil
	})
	return nil
}

func (u *Updates) applyUpdates(downloader Downloader, force bool) error {
	currentBundle := u.registry.bundle
	downloadBundle := downloader.bundle

	if !force && u.registry.version != nil {
		if u.downloader.version.LessThanOrEqual(u.registry.version) {
			// No new version, silently return.
			return nil
		}
	}
	if currentBundle != nil {
		log.Infof("update: starting update: %s %s -> %s", currentBundle.Name, currentBundle.Version, downloadBundle.Version)
	}

	err := u.registry.performRecoverableUpgrade(downloader.dir, downloader.indexFile)
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
	u.upgradeWorkerMgr.Go()
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

	if u.corruptedInstallation {
		notifications.NotifyError(corruptInstallationNotificationID, "Corrupted installation. Reinstall the software.", "")
	}

	u.updateCheckWorkerMgr.Go()

	return nil
}

func (u *Updates) GetRootPath() string {
	return u.registry.dir
}

// GetFile returns the path of a file given the name. Returns ErrNotFound if file is not found.
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
	Restart()
	Shutdown()
	Notifications() *notifications.Notifications
}
