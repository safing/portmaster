package updates

import (
	"fmt"
	"runtime"
	"time"

	"github.com/safing/portmaster/base/database"
)

const (
	onWindows = runtime.GOOS == "windows"

	enableSoftwareUpdatesKey = "core/automaticUpdates"
	enableIntelUpdatesKey    = "core/automaticIntelUpdates"

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
	userAgentFromFlag    string
	updateServerFromFlag string

	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	// UserAgent is an HTTP User-Agent that is used to add
	// more context to requests made by the registry when
	// fetching resources from the update server.
	UserAgent = fmt.Sprintf("Portmaster (%s %s)", runtime.GOOS, runtime.GOARCH)
)

const (
	updateTaskRepeatDuration = 1 * time.Hour
)

func stop() error {
	// if registry != nil {
	// 	err := registry.Cleanup()
	// 	if err != nil {
	// 		log.Warningf("updates: failed to clean up registry: %s", err)
	// 	}
	// }

	return nil
}

// RootPath returns the root path used for storing updates.
func RootPath() string {
	// if !module.Online() {
	// 	return ""
	// }

	// return registry.StorageDir().Path
	return ""
}
