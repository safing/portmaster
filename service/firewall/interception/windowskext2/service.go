//go:build windows
// +build windows

package windowskext

import "github.com/safing/portmaster/windows_kext/kextinterface"

func createKextService(driverName string, driverPath string) (*kextinterface.KextService, error) {
	return kextinterface.CreateKextService(driverName, driverPath)
}
