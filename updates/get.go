package updates

import (
	"path"

	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates/helper"
)

// GetPlatformFile returns the latest platform specific file identified by the given identifier.
func GetPlatformFile(identifier string) (*updater.File, error) {
	identifier = helper.PlatformIdentifier(identifier)

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
