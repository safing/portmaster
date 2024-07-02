package updates

import (
	"path"

	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/service/updates/helper"
)

// GetPlatformFile returns the latest platform specific file identified by the given identifier.
func GetPlatformFile(identifier string) (*updater.File, error) {
	identifier = helper.PlatformIdentifier(identifier)

	file, err := registry.GetFile(identifier)
	if err != nil {
		return nil, err
	}

	module.EventVersionsUpdated.Submit(struct{}{})
	return file, nil
}

// GetFile returns the latest generic file identified by the given identifier.
func GetFile(identifier string) (*updater.File, error) {
	identifier = path.Join("all", identifier)

	file, err := registry.GetFile(identifier)
	if err != nil {
		return nil, err
	}

	module.EventVersionsUpdated.Submit(struct{}{})
	return file, nil
}

// GetPlatformVersion returns the selected platform specific version of the
// given identifier.
// The returned resource version may not be modified.
func GetPlatformVersion(identifier string) (*updater.ResourceVersion, error) {
	identifier = helper.PlatformIdentifier(identifier)

	rv, err := registry.GetVersion(identifier)
	if err != nil {
		return nil, err
	}

	return rv, nil
}

// GetVersion returns the selected generic version of the given identifier.
// The returned resource version may not be modified.
func GetVersion(identifier string) (*updater.ResourceVersion, error) {
	identifier = path.Join("all", identifier)

	rv, err := registry.GetVersion(identifier)
	if err != nil {
		return nil, err
	}

	return rv, nil
}

// GetVersionWithFullID returns the selected generic version of the given full identifier.
// The returned resource version may not be modified.
func GetVersionWithFullID(identifier string) (*updater.ResourceVersion, error) {
	rv, err := registry.GetVersion(identifier)
	if err != nil {
		return nil, err
	}

	return rv, nil
}
