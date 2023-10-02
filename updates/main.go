package updates

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"runtime"
	"time"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates/helper"
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
	module   *modules.Module
	registry *updater.ResourceRegistry

	userAgentFromFlag    string
	updateServerFromFlag string

	updateTask          *modules.Task
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
	module = modules.Register(ModuleName, prep, start, stop, "base")
	module.RegisterEvent(VersionUpdateEvent, true)
	module.RegisterEvent(ResourceUpdateEvent, true)

	flag.StringVar(&updateServerFromFlag, "update-server", "", "set an alternative update server (full URL)")
	flag.StringVar(&userAgentFromFlag, "update-agent", "", "set an alternative user agent for requests to the update server")
}

func prep() error {
	if err := registerConfig(); err != nil {
		return err
	}

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

	return registerAPIEndpoints()
}

func start() error {
	initConfig()

	restartTask = module.NewTask("automatic restart", automaticRestart).MaxDelay(10 * time.Minute)

	if err := module.RegisterEventHook(
		"config",
		"config change",
		"update registry config",
		updateRegistryConfig); err != nil {
		return err
	}

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
	err = registry.Initialize(dataroot.Root().ChildDir(updatesDirName, 0o0755))
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

	err = registry.LoadIndexes(module.Ctx)
	if err != nil {
		log.Warningf("updates: failed to load indexes: %s", err)
	}

	err = registry.ScanStorage("")
	if err != nil {
		log.Warningf("updates: error during storage scan: %s", err)
	}

	registry.SelectVersions()
	module.TriggerEvent(VersionUpdateEvent, nil)

	// Initialize the version export - this requires the registry to be set up.
	err = initVersionExport()
	if err != nil {
		return err
	}

	// start updater task
	updateTask = module.NewTask("updater", func(ctx context.Context, task *modules.Task) error {
		return checkForUpdates(ctx)
	})

	if !disableTaskSchedule {
		updateTask.
			Repeat(updateTaskRepeatDuration).
			MaxDelay(30 * time.Minute)
	}

	if updateASAP {
		updateTask.StartASAP()
	}

	// react to upgrades
	if err := initUpgrader(); err != nil {
		return err
	}

	warnOnIncorrectParentPath()

	return nil
}

// TriggerUpdate queues the update task to execute ASAP.
func TriggerUpdate(force bool) error {
	switch {
	case !module.Online():
		updateASAP = true

	case !force && !enableSoftwareUpdates() && !enableIntelUpdates():
		return fmt.Errorf("automatic updating is disabled")

	default:
		if force {
			forceUpdate.Set()
		}
		updateTask.StartASAP()
	}

	log.Debugf("updates: triggering update to run as soon as possible")
	return nil
}

// DisableUpdateSchedule disables the update schedule.
// If called, updates are only checked when TriggerUpdate()
// is called.
func DisableUpdateSchedule() error {
	switch module.Status() {
	case modules.StatusStarting, modules.StatusOnline, modules.StatusStopping:
		return fmt.Errorf("module already online")
	}

	disableTaskSchedule = true

	return nil
}

func checkForUpdates(ctx context.Context) (err error) {
	// Set correct error if context was canceled.
	defer func() {
		select {
		case <-ctx.Done():
			err = context.Canceled
		default:
		}
	}()

	forcedUpdate := forceUpdate.SetToIf(true, false)
	if !forcedUpdate && !enableSoftwareUpdates() && !enableIntelUpdates() {
		log.Warningf("updates: automatic updates are disabled")
		return nil
	}

	defer func() {
		// Resolve any error and send success notification.
		if err == nil {
			log.Infof("updates: successfully checked for updates")
			notifyUpdateSuccess(forcedUpdate)
			return
		}

		// Log and notify error.
		log.Errorf("updates: check failed: %s", err)
		notifyUpdateCheckFailed(forcedUpdate, err)
	}()

	if err = registry.UpdateIndexes(ctx); err != nil {
		err = fmt.Errorf("failed to update indexes: %w", err)
		return //nolint:nakedret // TODO: Would "return err" work with the defer?
	}

	err = registry.DownloadUpdates(ctx, forcedUpdate)
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

	module.TriggerEvent(ResourceUpdateEvent, nil)
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
	if !module.Online() {
		return ""
	}

	return registry.StorageDir().Path
}
