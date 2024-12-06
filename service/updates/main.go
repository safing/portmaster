package updates

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"runtime"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates/helper"
)

const (
	onWindows = runtime.GOOS == "windows"

	enableSoftwareUpdatesKey = "core/automaticUpdates"
	enableIntelUpdatesKey    = "core/automaticIntelUpdates"

	// ModuleName is the name of the update module
	// and can be used when declaring module dependencies.
	ModuleName = "updates"

	// VersionUpdateEvent is emitted every time a new
	// version of a monitored resource is selected.
	// During module initialization VersionUpdateEvent
	// is also emitted.
	VersionUpdateEvent = "active version update"

	// ResourceUpdateEvent is emitted every time the
	// updater successfully performed a resource update.
	// ResourceUpdateEvent is emitted even if no new
	// versions are available. Subscribers are expected
	// to check if new versions of their resources are
	// available by checking File.UpgradeAvailable().
	ResourceUpdateEvent = "resource update"
)

var (
	registry *updater.ResourceRegistry

	userAgentFromFlag    string
	updateServerFromFlag string

	updateASAP          bool
	disableTaskSchedule bool

	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	// UserAgent is an HTTP User-Agent that is used to add
	// more context to requests made by the registry when
	// fetching resources from the update server.
	UserAgent = fmt.Sprintf("Portmaster (%s %s)", runtime.GOOS, runtime.GOARCH)

	// DefaultUpdateURLs defines the default base URLs of the update server.
	DefaultUpdateURLs = []string{
		"https://updates.safing.io",
	}

	// DisableSoftwareAutoUpdate specifies whether software updates should be disabled.
	// This is used on Android, as it will never require binary updates.
	DisableSoftwareAutoUpdate = false
)

const (
	updatesDirName = "updates"

	updateTaskRepeatDuration = 1 * time.Hour
)

func init() {
	flag.StringVar(&updateServerFromFlag, "update-server", "", "set an alternative update server (full URL)")
	flag.StringVar(&userAgentFromFlag, "update-agent", "", "set an alternative user agent for requests to the update server")
}

func prep() error {
	// Check if update server URL supplied via flag is a valid URL.
	if updateServerFromFlag != "" {
		u, err := url.Parse(updateServerFromFlag)
		if err != nil {
			return fmt.Errorf("supplied update server URL is invalid: %w", err)
		}
		if u.Scheme != "https" {
			return errors.New("supplied update server URL must use HTTPS")
		}
	}

	if err := registerConfig(); err != nil {
		return err
	}

	return registerAPIEndpoints()
}

func start() error {
	initConfig()

	module.restartWorkerMgr.Repeat(10 * time.Minute)
	module.instance.Config().EventConfigChange.AddCallback("update registry config", updateRegistryConfig)

	// create registry
	registry = &updater.ResourceRegistry{
		Name:             ModuleName,
		UpdateURLs:       DefaultUpdateURLs,
		UserAgent:        UserAgent,
		MandatoryUpdates: helper.MandatoryUpdates(),
		AutoUnpack:       helper.AutoUnpackUpdates(),
		Verification:     helper.VerificationConfig,
		DevMode:          devMode(),
		Online:           true,
	}
	// Override values from flags.
	if userAgentFromFlag != "" {
		registry.UserAgent = userAgentFromFlag
	}
	if updateServerFromFlag != "" {
		registry.UpdateURLs = []string{updateServerFromFlag}
	}

	// pre-init state
	updateStateExport, err := LoadStateExport()
	if err != nil {
		log.Debugf("updates: failed to load exported update state: %s", err)
	} else if updateStateExport.UpdateState != nil {
		err := registry.PreInitUpdateState(*updateStateExport.UpdateState)
		if err != nil {
			return err
		}
	}

	// initialize
	err = registry.Initialize(dataroot.Root().ChildDir(updatesDirName, utils.PublicReadPermission))
	if err != nil {
		return err
	}

	// register state provider
	err = registerRegistryStateProvider()
	if err != nil {
		return err
	}
	registry.StateNotifyFunc = pushRegistryState

	// Set indexes based on the release channel.
	warning := helper.SetIndexes(
		registry,
		initialReleaseChannel,
		true,
		enableSoftwareUpdates() && !DisableSoftwareAutoUpdate,
		enableIntelUpdates(),
	)
	if warning != nil {
		log.Warningf("updates: %s", warning)
	}

	err = registry.LoadIndexes(module.m.Ctx())
	if err != nil {
		log.Warningf("updates: failed to load indexes: %s", err)
	}

	err = registry.ScanStorage("")
	if err != nil {
		log.Warningf("updates: error during storage scan: %s", err)
	}

	registry.SelectVersions()
	module.EventVersionsUpdated.Submit(struct{}{})

	// Initialize the version export - this requires the registry to be set up.
	err = initVersionExport()
	if err != nil {
		return err
	}

	// start updater task
	if !disableTaskSchedule {
		_ = module.updateWorkerMgr.Repeat(30 * time.Minute)
	}

	if updateASAP {
		module.updateWorkerMgr.Go()
	}

	// react to upgrades
	if err := initUpgrader(); err != nil {
		return err
	}

	warnOnIncorrectParentPath()

	return nil
}

// TriggerUpdate queues the update task to execute ASAP.
func TriggerUpdate(forceIndexCheck, downloadAll bool) error {
	switch {
	case !forceIndexCheck && !enableSoftwareUpdates() && !enableIntelUpdates():
		return errors.New("automatic updating is disabled")

	default:
		if forceIndexCheck {
			forceCheck.Set()
		}
		if downloadAll {
			forceDownload.Set()
		}

		// If index check if forced, start quicker.
		module.updateWorkerMgr.Go()
	}

	log.Debugf("updates: triggering update to run as soon as possible")
	return nil
}

// DisableUpdateSchedule disables the update schedule.
// If called, updates are only checked when TriggerUpdate()
// is called.
func DisableUpdateSchedule() error {
	// TODO: Updater state should be always on
	// switch module.Status() {
	// case modules.StatusStarting, modules.StatusOnline, modules.StatusStopping:
	// 	return errors.New("module already online")
	// }

	disableTaskSchedule = true

	return nil
}

func checkForUpdates(ctx *mgr.WorkerCtx) (err error) {
	// Set correct error if context was canceled.
	defer func() {
		select {
		case <-ctx.Done():
			err = context.Canceled
		default:
		}
	}()

	// Get flags.
	forceIndexCheck := forceCheck.SetToIf(true, false)
	downloadAll := forceDownload.SetToIf(true, false)

	// Check again if downloading updates is enabled, or forced.
	if !forceIndexCheck && !enableSoftwareUpdates() && !enableIntelUpdates() {
		log.Warningf("updates: automatic updates are disabled")
		return nil
	}

	defer func() {
		// Resolve any error and send success notification.
		if err == nil {
			log.Infof("updates: successfully checked for updates")
			notifyUpdateSuccess(forceIndexCheck)
			return
		}

		// Log and notify error.
		log.Errorf("updates: check failed: %s", err)
		notifyUpdateCheckFailed(forceIndexCheck, err)
	}()

	if err = registry.UpdateIndexes(ctx.Ctx()); err != nil {
		err = fmt.Errorf("failed to update indexes: %w", err)
		return //nolint:nakedret // TODO: Would "return err" work with the defer?
	}

	err = registry.DownloadUpdates(ctx.Ctx(), downloadAll)
	if err != nil {
		err = fmt.Errorf("failed to download updates: %w", err)
		return //nolint:nakedret // TODO: Would "return err" work with the defer?
	}

	registry.SelectVersions()

	// Unpack selected resources.
	err = registry.UnpackResources()
	if err != nil {
		err = fmt.Errorf("failed to unpack updates: %w", err)
		return //nolint:nakedret // TODO: Would "return err" work with the defer?
	}

	// Purge old resources
	registry.Purge(2)

	module.EventResourcesUpdated.Submit(struct{}{})
	return nil
}

func stop() error {
	if registry != nil {
		err := registry.Cleanup()
		if err != nil {
			log.Warningf("updates: failed to clean up registry: %s", err)
		}
	}

	return nil
}

// RootPath returns the root path used for storing updates.
func RootPath() string {
	// if !module.Online() {
	// 	return ""
	// }

	return registry.StorageDir().Path
}
