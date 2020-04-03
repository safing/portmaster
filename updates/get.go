package updates

import (
	"fmt"
	"path"
	"runtime"

	"github.com/safing/portbase/updater"
)

// GetPlatformFile returns the latest platform specific file identified by the given identifier.
func GetPlatformFile(identifier string) (*updater.File, error) {
	identifier = path.Join(fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH), identifier)
	// From https://golang.org/pkg/runtime/#GOARCH
	// GOOS is the running program's operating system target: one of darwin, freebsd, linux, and so on.
	// GOARCH is the running program's architecture target: one of 386, amd64, arm, s390x, and so on.

	file, err := registry.GetFile(identifier)
	if err != nil {
		return nil, err
	}

	module.TriggerEvent(VersionUpdateEvent, nil)
	return file, nil
}

// GetFile returns the latest generic file identified by the given identifier.
func GetFile(identifier string) (*updater.File, error) {
	identifier = path.Join("all", identifier)

	file, err := registry.GetFile(identifier)
	if err != nil {
		return nil, err
	}

	module.TriggerEvent(VersionUpdateEvent, nil)
	return file, nil
}
