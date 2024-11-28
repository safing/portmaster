package helper

import (
	"fmt"
	"runtime"

	"github.com/tevino/abool"
)

const onWindows = runtime.GOOS == "windows"

var intelOnly = abool.New()

// IntelOnly specifies that only intel data is mandatory.
func IntelOnly() {
	intelOnly.Set()
}

// PlatformIdentifier converts identifier for the current platform.
func PlatformIdentifier(identifier string) string {
	// From https://golang.org/pkg/runtime/#GOARCH
	// GOOS is the running program's operating system target: one of darwin, freebsd, linux, and so on.
	// GOARCH is the running program's architecture target: one of 386, amd64, arm, s390x, and so on.
	return fmt.Sprintf("%s_%s/%s", runtime.GOOS, runtime.GOARCH, identifier)
}

// MandatoryUpdates returns mandatory updates that should be loaded on install
// or reset.
func MandatoryUpdates() (identifiers []string) {
	// Intel
	identifiers = append(
		identifiers,

		// Filter lists data
		"all/intel/lists/index.dsd",
		"all/intel/lists/base.dsdl",
		"all/intel/lists/intermediate.dsdl",
		"all/intel/lists/urgent.dsdl",

		// Geo IP data
		"all/intel/geoip/geoipv4.mmdb.gz",
		"all/intel/geoip/geoipv6.mmdb.gz",
	)

	// Stop here if we only want intel data.
	if intelOnly.IsSet() {
		return identifiers
	}

	// Binaries
	if onWindows {
		identifiers = append(
			identifiers,
			PlatformIdentifier("core/portmaster-core.exe"),
			PlatformIdentifier("dll/portmaster-core.dll"),
			PlatformIdentifier("kext/portmaster-kext.sys"),
			PlatformIdentifier("kext/portmaster-kext.pdb"),
			PlatformIdentifier("start/portmaster-start.exe"),
			PlatformIdentifier("notifier/portmaster-notifier.exe"),
			PlatformIdentifier("notifier/portmaster-wintoast.dll"),
			PlatformIdentifier("app2/portmaster-app.zip"),
		)
	} else {
		identifiers = append(
			identifiers,
			PlatformIdentifier("core/portmaster-core"),
			PlatformIdentifier("start/portmaster-start"),
			PlatformIdentifier("notifier/portmaster-notifier"),
			PlatformIdentifier("app2/portmaster-app"),
		)
	}

	// Components, Assets and Data
	identifiers = append(
		identifiers,

		// User interface components
		PlatformIdentifier("app/portmaster-app.zip"),
		"all/ui/modules/portmaster.zip",
		"all/ui/modules/assets.zip",
	)

	return identifiers
}

// AutoUnpackUpdates returns assets that need unpacking.
func AutoUnpackUpdates() []string {
	if intelOnly.IsSet() {
		return []string{}
	}

	return []string{
		PlatformIdentifier("app/portmaster-app.zip"),
		PlatformIdentifier("app2/portmaster-app.zip"),
	}
}
