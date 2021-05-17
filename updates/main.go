package updates

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portbase/updater"
)

const (
	onWindows = runtime.GOOS == "windows"

	releaseChannelKey    = "core/releaseChannel"
	releaseChannelStable = "stable"
	releaseChannelBeta   = "beta"

	enableUpdatesKey = "core/automaticUpdates"

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
	module            *modules.Module
	registry          *updater.ResourceRegistry
	userAgentFromFlag string
	staging           bool

	updateTask          *modules.Task
	updateASAP          bool
	disableTaskSchedule bool

	// MandatoryUpdates is a list of full identifiers that
	// should always be kept up to date.
	MandatoryUpdates []string

	// UserAgent is an HTTP User-Agent that is used to add
	// more context to requests made by the registry when
	// fetching resources from the update server.
	UserAgent = "Core"
)

const (
	updateInProgress = "updates:in-progress"
	updateFailed     = "updates:failed"
	updateSuccess    = "updates:success"
)

func init() {
	module = modules.Register(ModuleName, prep, start, stop, "base")
	module.RegisterEvent(VersionUpdateEvent, true)
	module.RegisterEvent(ResourceUpdateEvent, true)

	flag.StringVar(&userAgentFromFlag, "update-agent", "", "set the user agent for requests to the update server")
	flag.BoolVar(&staging, "staging", false, "use staging update channel; for testing only")

	// initialize mandatory updates
	if onWindows {
		MandatoryUpdates = []string{
			platform("core/portmaster-core.exe"),
			platform("start/portmaster-start.exe"),
			platform("notifier/portmaster-notifier.exe"),
			platform("notifier/portmaster-snoretoast.exe"),
		}
	} else {
		MandatoryUpdates = []string{
			platform("core/portmaster-core"),
			platform("start/portmaster-start"),
			platform("notifier/portmaster-notifier"),
		}
	}

	MandatoryUpdates = append(
		MandatoryUpdates,
		platform("app/portmaster-app.zip"),
		"all/ui/modules/portmaster.zip",
	)
}

func prep() error {
	if err := registerConfig(); err != nil {
		return err
	}

	return registerAPIEndpoints()

	return nil
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
		Name: ModuleName,
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
		UserAgent:        UserAgent,
		MandatoryUpdates: MandatoryUpdates,
		AutoUnpack: []string{
			platform("app/portmaster-app.zip"),
		},
		Beta:    releaseChannel() == releaseChannelBeta,
		DevMode: devMode(),
		Online:  true,
	}
	if userAgentFromFlag != "" {
		// override with flag value
		registry.UserAgent = userAgentFromFlag
	}
	// initialize
	err := registry.Initialize(dataroot.Root().ChildDir("updates", 0755))
	if err != nil {
		return err
	}

	registry.AddIndex(updater.Index{
		Path:   "stable.json",
		Stable: true,
		Beta:   false,
	})

	if registry.Beta {
		registry.AddIndex(updater.Index{
			Path:   "beta.json",
			Stable: false,
			Beta:   true,
		})
	}

	registry.AddIndex(updater.Index{
		Path:   "all/intel/intel.json",
		Stable: true,
		Beta:   true,
	})

	if stagingActive() {
		// Set flag no matter how staging was activated.
		staging = true

		log.Warning("updates: staging environment is active")

		registry.AddIndex(updater.Index{
			Path:   "staging.json",
			Stable: true,
			Beta:   true,
		})
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

	if !updatesCurrentlyEnabled {
		createWarningNotification()
	}

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
			Repeat(1 * time.Hour).
			MaxDelay(30 * time.Minute).
			Schedule(time.Now().Add(10 * time.Second))
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
	case !module.OnlineSoon():
		return fmt.Errorf("updates module is disabled")

	case !module.Online():
		updateASAP = true

	case !force && !enableUpdates():
		return fmt.Errorf("automatic updating is disabled")

	default:
		updateTask.StartASAP()
	}

	log.Debugf("updates: triggering update to run as soon as possible")
	return nil
}

// DisableUpdateSchedule disables the update schedule.
// If called, updates are only checked when TriggerUpdate()
// is called.
func DisableUpdateSchedule() error {
	if module.OnlineSoon() {
		return fmt.Errorf("module already online")
	}

	disableTaskSchedule = true

	return nil
}

func checkForUpdates(ctx context.Context) (err error) {
	if !updatesCurrentlyEnabled {
		log.Debugf("updates: automatic updates are disabled")
		return nil
	}
	defer log.Debugf("updates: finished checking for updates")

	defer func() {
		if err == nil {
			module.Resolve(updateFailed)
			notifications.Notify(&notifications.Notification{
				EventID: updateSuccess,
				Type:    notifications.Info,
				Title:   "Update Check Successful",
				Message: "The Portmaster successfully checked for updates and downloaded any available updates. Most updates are applied automatically. You will be notified of important updates that need restarting.",
				Expires: time.Now().Add(1 * time.Minute).Unix(),
				AvailableActions: []*notifications.Action{
					{
						ID:   "ack",
						Text: "OK",
					},
				},
			})
		} else {
			notifications.NotifyWarn(
				updateFailed,
				"Update Check Failed",
				"The Portmaster failed to check for updates. This might be a temporary issue of your device, your network or the update servers. The Portmaster will automatically try again later.",
				notifications.Action{
					ID:   "retry",
					Text: "Try Again Now",
					Type: notifications.ActionTypeWebhook,
					Payload: &notifications.ActionTypeWebhookPayload{
						URL:          apiPathCheckForUpdates,
						ResultAction: "display",
					},
				},
			).AttachToModule(module)
		}
	}()

	if err = registry.UpdateIndexes(ctx); err != nil {
		log.Warningf("updates: failed to update indexes: %s", err)
	}

	err = registry.DownloadUpdates(ctx)
	if err != nil {
		err = fmt.Errorf("failed to update: %w", err)
		return
	}

	registry.SelectVersions()

	// Unpack selected resources.
	err = registry.UnpackResources()
	if err != nil {
		err = fmt.Errorf("failed to update: %w", err)
		return
	}

	// Purge old resources
	registry.Purge(3)

	module.TriggerEvent(ResourceUpdateEvent, nil)
	return nil
}

func stop() error {
	if registry != nil {
		return registry.Cleanup()
	}

	return stopVersionExport()
}

func platform(identifier string) string {
	return fmt.Sprintf("%s_%s/%s", runtime.GOOS, runtime.GOARCH, identifier)
}

func stagingActive() bool {
	// Check flag and env variable.
	if staging || os.Getenv("PORTMASTER_STAGING") == "enabled" {
		return true
	}

	// Check if staging index is present and acessible.
	_, err := os.Stat(filepath.Join(registry.StorageDir().Path, "staging.json"))
	return err == nil
}

// RootPath returns the root path used for storing updates.
func RootPath() string {
	if !module.Online() {
		return ""
	}

	return registry.StorageDir().Path
}
