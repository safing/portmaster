package updates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/ui"
)

const (
	updateTaskRepeatDuration          = 1 * time.Hour
	noNewUpdateNotificationID         = "updates:no-new-update"
	updateAvailableNotificationID     = "updates:update-available"
	restartRequiredNotificationID     = "updates:restart-required"
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

	ErrAutoCheckDisabled = errors.New("automatic update checks are disabled")
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
	// Platform defines the platform to download artifacts for. Defaults to current platform.
	Platform string

	// AutoCheck defines that new indexes may be downloaded automatically without outside trigger.
	AutoCheck bool
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

	// Check platform.
	if cfg.Platform == "" {
		cfg.Platform = currentPlatform
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

	corruptedInstallation error

	isUpdateRunning *abool.AtomicBool
	started         *abool.AtomicBool
	configureLock   sync.Mutex

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
		started:         abool.NewBool(false),

		instance: instance,
	}

	// Check config.
	if err := module.cfg.Check(); err != nil {
		return nil, fmt.Errorf("config is invalid: %w", err)
	}

	// Make sure main dir exists.
	err := utils.EnsureDirectory(module.cfg.Directory, utils.PublicReadExecPermission)
	if err != nil {
		return nil, fmt.Errorf("create update target directory: %s", module.cfg.DownloadDirectory)
	}

	// Create Workers.
	module.updateCheckWorkerMgr = m.NewWorkerMgr("update checker", module.updateCheckWorker, nil)
	module.upgradeWorkerMgr = m.NewWorkerMgr("upgrader", module.upgradeWorker, nil)

	// Load index.
	index, err := LoadIndex(filepath.Join(cfg.Directory, cfg.IndexFile), cfg.Platform, cfg.Verify)
	if err == nil {
		// Verify artifacts.
		if err := index.VerifyArtifacts(cfg.Directory); err != nil {
			module.corruptedInstallation = fmt.Errorf("invalid artifact: %w", err)
		}

		// Save index to module and return.
		module.index = index
		return module, nil
	}

	// Fall back to scanning the directory.
	if !errors.Is(err, os.ErrNotExist) {
		log.Errorf("updates/%s: invalid index file, falling back to dir scan: %s", cfg.Name, err)
		module.corruptedInstallation = fmt.Errorf("invalid index: %w", err)
	}
	index, err = GenerateIndexFromDir(cfg.Directory, IndexScanConfig{
		Name:    configure.DefaultBinaryIndexName,
		Version: info.VersionNumber(),
	})
	if err == nil && index.init(currentPlatform) == nil {
		module.index = index
		return module, nil
	}

	// Fall back to empty index.
	return module, nil
}

func (u *Updater) updateAndUpgrade(w *mgr.WorkerCtx, indexURLs []string, ignoreVersion, forceApply bool) (err error) { //nolint:maintidx
	// Make sure only one update process is running.
	if !u.isUpdateRunning.SetToIf(false, true) {
		return fmt.Errorf("an updater task is already running, please try again later")
	}
	defer u.isUpdateRunning.UnSet()

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
		downloader.index, err = LoadIndex(filepath.Join(u.cfg.DownloadDirectory, u.cfg.IndexFile), u.cfg.Platform, u.cfg.Verify)
		if err != nil {
			return fmt.Errorf("load previously downloaded index file: %w", err)
		}
	}

	// Get index to check version.
	u.indexLock.Lock()
	index := u.index
	u.indexLock.Unlock()

	// Check if there is a new version.
	if !ignoreVersion && index != nil {
		// Check with local pointer to index.
		if err := index.ShouldUpgradeTo(downloader.index); err != nil {
			if errors.Is(err, ErrSameIndex) {
				log.Infof("updates/%s: no new update", u.cfg.Name)
				if u.cfg.Notify && u.instance.Notifications() != nil {
					u.instance.Notifications().Notify(&notifications.Notification{
						EventID: noNewUpdateNotificationID,
						Type:    notifications.Info,
						Title:   "Portmaster Is Up-To-Date",
						Message: "Portmaster v" + index.Version + " is the newest version.",
						Expires: time.Now().Add(1 * time.Minute).Unix(),
						AvailableActions: []*notifications.Action{
							{
								ID:   "ack",
								Text: "OK",
							},
						},
					})
				}
			} else {
				log.Warningf("updates/%s: cannot update: %s", u.cfg.Name, err)
				if u.cfg.Notify && u.instance.Notifications() != nil {
					u.instance.Notifications().Notify(&notifications.Notification{
						EventID: noNewUpdateNotificationID,
						Type:    notifications.Info,
						Title:   "Portmaster Is Up-To-Date*",
						Message: "While Portmaster v" + index.Version + " is the newest version, there is an internal issue with checking for updates: " + err.Error(),
						Expires: time.Now().Add(1 * time.Minute).Unix(),
						AvailableActions: []*notifications.Action{
							{
								ID:   "ack",
								Text: "OK",
							},
						},
					})
				}
			}
			return fmt.Errorf("%w: %w", ErrNoUpdateAvailable, err)
		}
	}

	// Check if automatic downloads are enabled.
	if !u.cfg.AutoDownload && !forceApply {
		log.Infof("updates/%s: new update to v%s available, action required to download and upgrade", u.cfg.Name, downloader.index.Version)
		if u.cfg.Notify && u.instance.Notifications() != nil {
			u.instance.Notifications().Notify(&notifications.Notification{
				EventID: updateAvailableNotificationID,
				Type:    notifications.Info,
				Title:   "New Update Available",
				Message: "Portmaster v" + downloader.index.Version + " is available. Click Upgrade to download and upgrade now.",
				AvailableActions: []*notifications.Action{
					{
						ID:   "ack",
						Text: "OK",
					},
					{
						ID:   "upgrade",
						Text: "Upgrade Now",
						Type: notifications.ActionTypeWebhook,
						Payload: notifications.ActionTypeWebhookPayload{
							Method: "POST",
							URL:    "updates/apply",
						},
					},
				},
			})
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
		log.Infof("updates/%s: new update to v%s available, action required to upgrade", u.cfg.Name, downloader.index.Version)
		if u.cfg.Notify && u.instance.Notifications() != nil {
			u.instance.Notifications().Notify(&notifications.Notification{
				EventID: updateAvailableNotificationID,
				Type:    notifications.Info,
				Title:   "New Update Ready",
				Message: "Portmaster v" + downloader.index.Version + " is available. Click Upgrade to upgrade now.",
				AvailableActions: []*notifications.Action{
					{
						ID:   "ack",
						Text: "OK",
					},
					{
						ID:   "upgrade",
						Text: "Upgrade Now",
						Type: notifications.ActionTypeWebhook,
						Payload: notifications.ActionTypeWebhookPayload{
							Method: "POST",
							URL:    "updates/apply",
						},
					},
				},
			})
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
	err = u.cleanupAfterUpgrade()
	if err != nil {
		log.Debugf("updates/%s: failed to clean up after upgrade: %s", u.cfg.Name, err)
	}
	u.EventResourcesUpdated.Submit(struct{}{})

	// If no restart is needed, we are done.
	if !u.cfg.NeedsRestart {
		return nil
	}

	// Notify user that a restart is required.
	if u.cfg.Notify {
		if u.instance.Notifications() != nil {
			u.instance.Notifications().Notify(&notifications.Notification{
				EventID: restartRequiredNotificationID,
				Type:    notifications.Info,
				Title:   "Restart Required",
				Message: "Portmaster v" + downloader.index.Version + " is installed. Restart to use new version.",
				AvailableActions: []*notifications.Action{
					{
						ID:   "ack",
						Text: "Later",
					},
					{
						ID:   "restart",
						Text: "Restart Now",
						Type: notifications.ActionTypeWebhook,
						Payload: notifications.ActionTypeWebhookPayload{
							Method: "POST",
							URL:    "updates/apply",
						},
					},
				},
			})
		}

		return fmt.Errorf("%w: restart required", ErrActionRequired)
	}

	// Otherwise, trigger restart immediately.
	u.instance.Restart()
	return nil
}

func (u *Updater) getIndexURLsWithLock() []string {
	u.configureLock.Lock()
	defer u.configureLock.Unlock()

	return u.cfg.IndexURLs
}

func (u *Updater) updateCheckWorker(w *mgr.WorkerCtx) error {
	err := u.updateAndUpgrade(w, u.getIndexURLsWithLock(), false, false)
	switch {
	case err == nil:
		return nil // Success!
	case errors.Is(err, ErrSameIndex):
		return nil // Nothing to do.
	case errors.Is(err, ErrNoUpdateAvailable):
		return nil // Already logged.
	case errors.Is(err, ErrActionRequired) && !u.cfg.Notify:
		return fmt.Errorf("user action required, but notifying user is disabled: %w", err)
	default:
		return fmt.Errorf("udpating failed: %w", err)
	}
}

func (u *Updater) upgradeWorker(w *mgr.WorkerCtx) error {
	err := u.updateAndUpgrade(w, u.getIndexURLsWithLock(), false, true)
	switch {
	case err == nil:
		return nil // Success!
	case errors.Is(err, ErrSameIndex):
		return nil // Nothing to do.
	case errors.Is(err, ErrNoUpdateAvailable):
		return nil // Already logged.
	case errors.Is(err, ErrActionRequired) && !u.cfg.Notify:
		return fmt.Errorf("user action required, but notifying user is disabled: %w", err)
	default:
		return fmt.Errorf("udpating failed: %w", err)
	}
}

// ForceUpdate executes a forced update and upgrade directly and synchronously
// and is intended to be used only within a tool, not a service.
func (u *Updater) ForceUpdate() error {
	return u.m.Do("update and upgrade", func(w *mgr.WorkerCtx) error {
		return u.updateAndUpgrade(w, u.getIndexURLsWithLock(), true, true)
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

// Configure makes slight configuration changes to the updater.
// It locks the index, which can take a while an update is running.
func (u *Updater) Configure(autoCheck bool, indexURLs []string) {
	u.configureLock.Lock()
	defer u.configureLock.Unlock()

	// Apply new config.
	var changed bool
	if u.cfg.AutoCheck != autoCheck {
		u.cfg.AutoCheck = autoCheck
		changed = true
	}
	if !slices.Equal(u.cfg.IndexURLs, indexURLs) {
		u.cfg.IndexURLs = indexURLs
		changed = true
	}

	// Trigger update check if enabled and something changed.
	if changed && u.started.IsSet() {
		if autoCheck {
			u.updateCheckWorkerMgr.Repeat(updateTaskRepeatDuration).Go()
		} else {
			u.updateCheckWorkerMgr.Repeat(0)
		}
	}
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
	u.configureLock.Lock()
	defer u.configureLock.Unlock()

	if u.corruptedInstallation != nil && u.cfg.Notify && u.instance.Notifications() != nil {
		u.states.Add(mgr.State{
			ID:      corruptInstallationNotificationID,
			Name:    "Install Corruption",
			Message: "Portmaster has detected that one or more of its own files have been corrupted. Please re-install the software. Error: " + u.corruptedInstallation.Error(),
			Type:    mgr.StateTypeError,
			Data:    u.corruptedInstallation,
		})
	}

	// Check for updates automatically, if enabled.
	if u.cfg.AutoCheck {
		u.updateCheckWorkerMgr.
			Repeat(updateTaskRepeatDuration).
			Delay(15 * time.Second)
	}

	u.started.SetTo(true)
	return nil
}

func (u *Updater) GetMainDir() string {
	return u.cfg.Directory
}

// GetIndex returns a copy of the index.
func (u *Updater) GetIndex() (*Index, error) {
	// Copy Artifacts.
	artifacts, err := u.GetFiles()
	if err != nil {
		return nil, err
	}

	u.indexLock.Lock()
	defer u.indexLock.Unlock()

	// Check if any index is active.
	if u.index == nil {
		return nil, ErrNotFound
	}

	return &Index{
		Name:       u.index.Name,
		Version:    u.index.Version,
		Published:  u.index.Published,
		Artifacts:  artifacts,
		versionNum: u.index.versionNum,
	}, nil
}

// GetFiles returns all artifacts. Returns ErrNotFound if no artifacts are found.
func (u *Updater) GetFiles() ([]*Artifact, error) {
	u.indexLock.Lock()
	defer u.indexLock.Unlock()

	// Check if any index is active.
	if u.index == nil {
		return nil, ErrNotFound
	}

	// Export all artifacts.
	export := make([]*Artifact, 0, len(u.index.Artifacts))
	for _, artifact := range u.index.Artifacts {
		switch {
		case artifact.Platform != "" && artifact.Platform != u.cfg.Platform:
			// Platform is defined and does not match.
			// Platforms are usually pre-filtered, but just to be sure.
		default:
			// Artifact matches!
			export = append(export, artifact.export(u.cfg.Directory, u.index.versionNum))
		}
	}

	// Check if anything was exported.
	if len(export) == 0 {
		return nil, ErrNotFound
	}

	return export, nil
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
		case artifact.Platform != "" && artifact.Platform != u.cfg.Platform:
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
	u.started.SetTo(false)
	return nil
}

type instance interface {
	Restart()
	Shutdown()
	Notifications() *notifications.Notifications
	UI() *ui.UI
}
