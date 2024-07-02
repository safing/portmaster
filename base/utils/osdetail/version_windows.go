package osdetail

import (
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/go-version"
	"github.com/shirou/gopsutil/host"
)

var (
	// versionRe = regexp.MustCompile(`[0-9\.]+`)

	windowsNTVersion       string
	windowsNTVersionForCmp *version.Version

	fetching sync.Mutex
	fetched  bool
)

// WindowsNTVersion returns the current Windows version.
func WindowsNTVersion() (string, error) {
	var err error
	fetching.Lock()
	defer fetching.Unlock()

	if !fetched {
		_, _, windowsNTVersion, err = host.PlatformInformation()

		windowsNTVersion = strings.SplitN(windowsNTVersion, " ", 2)[0]

		if err != nil {
			return "", fmt.Errorf("failed to obtain Windows-Version: %s", err)
		}

		windowsNTVersionForCmp, err = version.NewVersion(windowsNTVersion)

		if err != nil {
			return "", fmt.Errorf("failed to parse Windows-Version %s: %s", windowsNTVersion, err)
		}

		fetched = true
	}

	return windowsNTVersion, err
}

// IsAtLeastWindowsNTVersion returns whether the current WindowsNT version is at least the given version or newer.
func IsAtLeastWindowsNTVersion(v string) (bool, error) {
	_, err := WindowsNTVersion()
	if err != nil {
		return false, err
	}

	versionForCmp, err := version.NewVersion(v)
	if err != nil {
		return false, err
	}

	return windowsNTVersionForCmp.GreaterThanOrEqual(versionForCmp), nil
}

// IsAtLeastWindowsNTVersionWithDefault is like IsAtLeastWindowsNTVersion(), but keeps the Error and returns the default Value in Errorcase
func IsAtLeastWindowsNTVersionWithDefault(v string, defaultValue bool) bool {
	val, err := IsAtLeastWindowsNTVersion(v)
	if err != nil {
		return defaultValue
	}
	return val
}

// IsAtLeastWindowsVersion returns whether the current Windows version is at least the given version or newer.
func IsAtLeastWindowsVersion(v string) (bool, error) {
	var NTVersion string
	switch v {
	case "7":
		NTVersion = "6.1"
	case "8":
		NTVersion = "6.2"
	case "8.1":
		NTVersion = "6.3"
	case "10":
		NTVersion = "10"
	default:
		return false, fmt.Errorf("failed to compare Windows-Version: Windows %s is unknown", v)
	}

	return IsAtLeastWindowsNTVersion(NTVersion)
}

// IsAtLeastWindowsVersionWithDefault is like IsAtLeastWindowsVersion(), but keeps the Error and returns the default Value in Errorcase
func IsAtLeastWindowsVersionWithDefault(v string, defaultValue bool) bool {
	val, err := IsAtLeastWindowsVersion(v)
	if err != nil {
		return defaultValue
	}
	return val
}
