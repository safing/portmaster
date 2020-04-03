package updates

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/updater"
)

const (
	onWindows = runtime.GOOS == "windows"

	releaseChannelKey    = "core/releaseChannel"
	releaseChannelStable = "stable"
	releaseChannelBeta   = "beta"

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
)

func init() {
	module = modules.Register(ModuleName, registerConfig, start, stop, "base")
	module.RegisterEvent(VersionUpdateEvent)
	module.RegisterEvent(ResourceUpdateEvent)
}

func start() error {
	initConfig()

	var mandatoryUpdates []string
	if onWindows {
		mandatoryUpdates = []string{
			platform("core/portmaster-core.exe"),
			platform("control/portmaster-control.exe"),
			platform("app/portmaster-app.exe"),
			platform("notifier/portmaster-notifier.exe"),
			platform("notifier/portmaster-snoretoast.exe"),
		}
	} else {
		mandatoryUpdates = []string{
			platform("core/portmaster-core"),
			platform("control/portmaster-control"),
			platform("app/portmaster-app"),
			platform("notifier/portmaster-notifier"),
		}
	}

	// create registry
	registry = &updater.ResourceRegistry{
		Name: ModuleName,
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
		MandatoryUpdates: mandatoryUpdates,
		Beta:             releaseChannel() == releaseChannelBeta,
		DevMode:          devMode(),
		Online:           true,
	}
	// initialize
	err := registry.Initialize(dataroot.Root().ChildDir("updates", 0755))
	if err != nil {
		return err
	}

	err = registry.LoadIndexes()
	if err != nil {
		return err
	}

	err = registry.ScanStorage("")
	if err != nil {
		log.Warningf("updates: error during storage scan: %s", err)
	}

	registry.SelectVersions()
	module.TriggerEvent(VersionUpdateEvent, nil)

	err = initVersionExport()
	if err != nil {
		return err
	}

	// start updater task
	module.NewTask("updater", func(ctx context.Context, task *modules.Task) error {
		err := registry.DownloadUpdates(ctx)
		if err != nil {
			return fmt.Errorf("updates: failed to update: %s", err)
		}
		module.TriggerEvent(ResourceUpdateEvent, nil)
		return nil
	}).Repeat(24 * time.Hour).MaxDelay(1 * time.Hour).Schedule(time.Now().Add(10 * time.Second))

	// react to upgrades
	return initUpgrader()
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
