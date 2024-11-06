package updates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/tevino/abool"
)

const (
	updateTaskRepeatDuration          = 1 * time.Hour
	noNewUpdateNotificationID         = "updates:no-new-update"
	updateAvailableNotificationID     = "updates:update-available"
	updateFailedNotificationID        = "updates:update-failed"
	corruptInstallationNotificationID = "updates:corrupt-installation"

	// ResourceUpdateEvent is emitted every time the
	// updater successfully performed a resource update.
	ResourceUpdateEvent = "resource update"
)

// UserAgent is an HTTP User-Agent that is used to add
// more context to requests made by the registry when
// fetching resources from the update server.
var UserAgent = fmt.Sprintf("Portmaster (%s %s)", runtime.GOOS, runtime.GOARCH)

// Errors.
var (
	ErrNotFound  = errors.New("file not found")
	ErrSameIndex = errors.New("same index")

	ErrNoUpdateAvailable = errors.New("no update available")
	ErrActionRequired    = errors.New("action required")
)

// Config holds the configuration for the updates module.
type Config struct {
	// Name of the updater.
	Name string
	// Directory is the main directory where the currently to-be-used artifacts live.
	Directory string
	// DownloadDirectory is the directory where new artifacts are downloaded to and prepared for upgrading.
	// After the upgrade, this directory is cleared.
	DownloadDirectory string
	// PurgeDirectory is the directory where old artifacts are moved to during the upgrade process.
	// After the upgrade, this directory is cleared.
	PurgeDirectory string
	// Ignore defines file and directory names within the main directory that should be ignored during the upgrade.
	Ignore []string

	// IndexURLs defines file
	IndexURLs []string
	// IndexFile is the name of the index file used in the directories.
	IndexFile string
	// Verify enables and specifies the trust the index signatures will be checked against.
	Verify jess.TrustStore

	// AutoDownload defines that updates may be downloaded automatically without outside trigger.
	AutoDownload bool
	// AutoApply defines that updates may be automatically applied without outside trigger.
	// Requires AutoDownload the be enabled.
	AutoApply bool
	// NeedsRestart defines that a restart is required after an upgrade has been completed.
	// Restart is triggered automatically, if Notify is disabled.
	NeedsRestart bool
	// Notify defines whether the user shall be informed about events via notifications.
	// If enabled, disables automatic restart after upgrade.
	Notify bool
}

// Check looks for obvious configuration errors.
func (cfg *Config) Check() error {
	// Check if required fields are set.
	switch {
	case cfg.Name == "":
		return errors.New("name must be set")
	case cfg.Directory == "":
		return errors.New("directory must be set")
	case cfg.DownloadDirectory == "":
		return errors.New("download directory must be set")
	case cfg.PurgeDirectory == "":
		return errors.New("purge directory must be set")
	case cfg.IndexFile == "":
		return errors.New("index file must be set")
	case cfg.AutoApply && !cfg.AutoDownload:
		return errors.New("auto apply is set, but auto download is not")
	}

	// Check if Ignore contains paths.
	for i, s := range cfg.Ignore {
		if strings.ContainsRune(s, filepath.Separator) {
			return fmt.Errorf("ignore entry #%d invalid: must be file or directory name, not path", i+1)
		}
	}

	// Check if IndexURLs are HTTPS.
	for i, url := range cfg.IndexURLs {
		if !strings.HasPrefix(url, "https://") {
			return fmt.Errorf("index URL #%d invalid: is not a HTTPS url", i+1)
		}
	}

	return nil
}

// Updater provides access to released artifacts.
type Updater struct {
	m      *mgr.Manager
	states *mgr.StateMgr
	cfg    Config

	index     *Index
	indexLock sync.Mutex

	updateCheckWorkerMgr *mgr.WorkerMgr
	upgradeWorkerMgr     *mgr.WorkerMgr

	EventResourcesUpdated *mgr.EventMgr[struct{}]

	corruptedInstallation bool

	isUpdateRunning *abool.AtomicBool

	instance instance
}

// New returns a new Updates module.
func New(instance instance, name string, cfg Config) (*Updater, error) {
	m := mgr.New(name)
	module := &Updater{
		m:      m,
		states: m.NewStateMgr(),
		cfg:    cfg,

		EventResourcesUpdated: mgr.NewEventMgr[struct{}](ResourceUpdateEvent, m),

		isUpdateRunning: abool.NewBool(false),

		instance: instance,
	}

	// Check config.
	if err := module.cfg.Check(); err != nil {
		return nil, fmt.Errorf("config is invalid: %w", err)
	}

	// Create Workers.
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.updateCheckWorker, nil).
		Repeat(updateTaskRepeatDuration)
	module.upgradeWorkerMgr = m.NewWorkerMgr("upgrader", module.upgradeWorker, nil)

	// Load index.
	index, err := LoadIndex(filepath.Join(cfg.Directory, cfg.IndexFile), cfg.Verify)
	if err == nil {
		module.index = index
		return module, nil
	}

	// Fall back to scanning the directory.
	if !errors.Is(err, os.ErrNotExist) {
		log.Errorf("updates/%s: invalid index file, falling back to dir scan: %w", cfg.Name, err)
	}
	index, err = GenerateIndexFromDir(cfg.Directory, IndexScanConfig{Version: "0.0.0"})
	if err == nil && index.init() == nil {
		module.index = index
		return module, nil
	}

	// Fall back to empty index.
	return module, nil
}

func (u *Updater) updateAndUpgrade(w *mgr.WorkerCtx, indexURLs []string, ignoreVersion, forceApply bool) (err error) {
	// Make sure only one update process is running.
	if !u.isUpdateRunning.SetToIf(false, true) {
		return fmt.Errorf("an updater task is already running, please try again later")
	}
	defer u.isUpdateRunning.UnSet()
	// FIXME: Switch to mutex?

	// Create a new downloader.
	downloader := NewDownloader(u, indexURLs)

	// Update or load the index file.
	if len(indexURLs) > 0 {
		// Download fresh copy, if indexURLs are given.
		err = downloader.updateIndex(w.Ctx())
		if err != nil {
			return fmt.Errorf("update index file: %w", err)
		}
	} else {
		// Otherwise, load index from download dir.
		downloader.index, err = LoadIndex(filepath.Join(u.cfg.Directory, u.cfg.IndexFile), u.cfg.Verify)
		if err != nil {
			return fmt.Errorf("load previously downloaded index file: %w", err)
		}
	}

	// Check if there is a new version.
	if !ignoreVersion {
		// Get index to check version.
		u.indexLock.Lock()
		index := u.index
		u.indexLock.Unlock()
		// Check with local pointer to index.
		if err := index.ShouldUpgradeTo(downloader.index); err != nil {
			log.Infof("updates/%s: no new or eligible update: %s", u.cfg.Name, err)
			if u.cfg.Notify && u.instance.Notifications() != nil {
				u.instance.Notifications().NotifyInfo(
					noNewUpdateNotificationID,
					"No Updates Available",
					"Portmaster v"+u.index.Version+" is the newest version.",
				)
			}
			return ErrNoUpdateAvailable
		}
	}

	// Check if automatic downloads are enabled.
	if !u.cfg.AutoDownload && !forceApply {
		if u.cfg.Notify && u.instance.Notifications() != nil {
			u.instance.Notifications().NotifyInfo(
				updateAvailableNotificationID,
				"New Update",
				"Portmaster v"+downloader.index.Version+" is available. Click Upgrade to download and upgrade now.",
				notifications.Action{
					ID:   "upgrade",
					Text: "Upgrade Now",
					Type: notifications.ActionTypeWebhook,
					Payload: notifications.ActionTypeWebhookPayload{
						Method: "POST",
						URL:    "updates/apply",
					},
				},
			)
		}
		return fmt.Errorf("%w: apply updates to download and upgrade", ErrActionRequired)
	}

	// Check for existing resources before starting to download.
	_ = downloader.gatherExistingFiles(u.cfg.Directory)         // Artifacts are re-used between versions.
	_ = downloader.gatherExistingFiles(u.cfg.DownloadDirectory) // Previous download may have been interrupted.
	_ = downloader.gatherExistingFiles(u.cfg.PurgeDirectory)    // Revover faster from a failed upgrade.

	// Download any remaining needed files.
	// If everything is already found in the download directory, then this is a no-op.
	log.Infof("updates/%s: downloading new version: %s %s", u.cfg.Name, downloader.index.Name, downloader.index.Version)
	err = downloader.downloadArtifacts(w.Ctx())
	if err != nil {
		log.Errorf("updates/%s: failed to download update: %s", u.cfg.Name, err)
		if err := u.deleteUnfinishedFiles(u.cfg.DownloadDirectory); err != nil {
			log.Debugf("updates/%s: failed to delete unfinished files in download directory %s", u.cfg.Name, u.cfg.DownloadDirectory)
		}
		return fmt.Errorf("downloading failed: %w", err)
	}

	// Notify the user that an upgrade is available.
	if !u.cfg.AutoApply && !forceApply {
		if u.cfg.Notify && u.instance.Notifications() != nil {
			u.instance.Notifications().NotifyInfo(
				updateAvailableNotificationID,
				"New Update",
				"Portmaster v"+downloader.index.Version+" is available. Click Upgrade to upgrade now.",
				notifications.Action{
					ID:   "upgrade",
					Text: "Upgrade Now",
					Type: notifications.ActionTypeWebhook,
					Payload: notifications.ActionTypeWebhookPayload{
						Method: "POST",
						URL:    "updates/apply",
					},
				},
			)
		}
		return fmt.Errorf("%w: apply updates to download and upgrade", ErrActionRequired)
	}

	// Run upgrade procedure.
	err = u.upgrade(downloader, ignoreVersion)
	if err != nil {
		if err := u.deleteUnfinishedFiles(u.cfg.PurgeDirectory); err != nil {
			log.Debugf("updates/%s: failed to delete unfinished files in purge directory %s", u.cfg.Name, u.cfg.PurgeDirectory)
		}
		return err
	}

	// Install is complete!

	// Clean up and notify modules of changed files.
	u.cleanupAfterUpgrade()
	u.EventResourcesUpdated.Submit(struct{}{})

	// If no restart is needed, we are done.
	if !u.cfg.NeedsRestart {
		return nil
	}

	// Notify user that a restart is required.
	if u.cfg.Notify && u.instance.Notifications() != nil {
		u.instance.Notifications().NotifyInfo(
			updateAvailableNotificationID,
			"Restart Required",
			"Portmaster v"+downloader.index.Version+" is installed. Restart to use new version.",
			notifications.Action{
				ID:   "restart",
				Text: "Restart Now",
				Type: notifications.ActionTypeWebhook,
				Payload: notifications.ActionTypeWebhookPayload{
					Method: "POST",
					URL:    "updates/apply", // FIXME
				},
			},
		)
		return fmt.Errorf("%w: restart required", ErrActionRequired)
	}

	// Otherwise, trigger restart immediately.
	u.instance.Restart()
	return nil
}

func (u *Updater) updateCheckWorker(w *mgr.WorkerCtx) error {
	_ = u.updateAndUpgrade(w, u.cfg.IndexURLs, false, false)
	// FIXME: Handle errors.
	return nil
}

func (u *Updater) upgradeWorker(w *mgr.WorkerCtx) error {
	_ = u.updateAndUpgrade(w, u.cfg.IndexURLs, false, true)
	// FIXME: Handle errors.
	return nil
}

// ForceUpdate executes a forced update and upgrade directly and synchronously
// and is intended to be used only within a tool, not a service.
func (u *Updater) ForceUpdate() error {
	return u.m.Do("update and upgrade", func(w *mgr.WorkerCtx) error {
		return u.updateAndUpgrade(w, u.cfg.IndexURLs, true, true)
	})
}

// UpdateFromURL installs an update from the provided url.
func (u *Updater) UpdateFromURL(url string) error {
	u.m.Go("custom update from url", func(w *mgr.WorkerCtx) error {
		_ = u.updateAndUpgrade(w, []string{url}, true, true)
		return nil
	})

	return nil
}

// TriggerUpdateCheck triggers an update check.
func (u *Updater) TriggerUpdateCheck() {
	u.updateCheckWorkerMgr.Go()
}

// TriggerApplyUpdates triggers upgrade.
func (u *Updater) TriggerApplyUpdates() {
	u.upgradeWorkerMgr.Go()
}

// States returns the state manager.
func (u *Updater) States() *mgr.StateMgr {
	return u.states
}

// Manager returns the module manager.
func (u *Updater) Manager() *mgr.Manager {
	return u.m
}

// Start starts the module.
func (u *Updater) Start() error {
	if u.corruptedInstallation && u.cfg.Notify && u.instance.Notifications() != nil {
		u.instance.Notifications().NotifyError(
			corruptInstallationNotificationID,
			"Install Corruption",
			"Portmaster has detected that one or more of its own files have been corrupted. Please re-install the software.",
		)
	}

	u.updateCheckWorkerMgr.Delay(15 * time.Second)
	return nil
}

func (u *Updater) GetMainDir() string {
	return u.cfg.Directory
}

// GetFile returns the path of a file given the name. Returns ErrNotFound if file is not found.
func (u *Updater) GetFile(name string) (*Artifact, error) {
	u.indexLock.Lock()
	defer u.indexLock.Unlock()

	// Check if any index is active.
	if u.index == nil {
		return nil, ErrNotFound
	}

	for _, artifact := range u.index.Artifacts {
		switch {
		case artifact.Filename != name:
			// Name does not match.
		case artifact.Platform != "" && artifact.Platform != currentPlatform:
			// Platform is defined and does not match.
			// Platforms are usually pre-filtered, but just to be sure.
		default:
			// Artifact matches!
			return artifact.export(u.cfg.Directory, u.index.versionNum), nil
		}
	}

	return nil, ErrNotFound
}

// Stop stops the module.
func (u *Updater) Stop() error {
	return nil
}

type instance interface {
	Restart()
	Shutdown()
	Notifications() *notifications.Notifications
}
