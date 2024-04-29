//go:build windows
// +build windows

package windowskext

import "github.com/safing/portmaster/windows_kext/kext_interface"

func createKextService(driverName string, driverPath string) (*kext_interface.KextService, error) {
	return kext_interface.CreateKextService(driverName, driverPath)
}
