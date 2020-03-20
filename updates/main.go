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

	eventVersionUpdate  = "active version update"
	eventResourceUpdate = "resource update"
)

var (
	module   *modules.Module
	registry *updater.ResourceRegistry
)

func init() {
	module = modules.Register("updates", registerConfig, start, stop, "core")
	module.RegisterEvent(eventVersionUpdate)
	module.RegisterEvent(eventResourceUpdate)
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
		Name: "updates",
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
	module.TriggerEvent(eventVersionUpdate, nil)

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
		module.TriggerEvent(eventResourceUpdate, nil)
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
