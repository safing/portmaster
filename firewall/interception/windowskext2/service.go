//go:build windows
// +build windows

package windowskext

import (
	"github.com/vlabo/portmaster_windows_rust_kext/kext_interface"
)

func createKextService(driverName string, driverPath string) (*kext_interface.KextService, error) {
	return kext_interface.CreateKextService(driverName, driverPath)
}
