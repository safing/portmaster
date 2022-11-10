package interception

import (
	"fmt"

	"github.com/safing/portmaster/firewall/interception/windowskext"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/updates"
)

// start starts the interception.
func start(ch chan packet.Packet) error {
	kextFile, err := updates.GetPlatformFile("kext/portmaster-kext.sys")
	if err != nil {
		return fmt.Errorf("interception: could not get kext sys: %s", err)
	}

	err = windowskext.Init(kextFile.Path())
	if err != nil {
		return fmt.Errorf("interception: could not init windows kext: %s", err)
	}

	err = windowskext.Start()
	if err != nil {
		return fmt.Errorf("interception: could not start windows kext: %s", err)
	}

	go windowskext.Handler(ch)

	return nil
}

// stop starts the interception.
func stop() error {
	return windowskext.Stop()
}

// ResetVerdictOfAllConnections resets all connections so they are forced to go thought the firewall again.
func ResetVerdictOfAllConnections() error {
	return windowskext.ClearCache()
}

// UpdateVerdictOfConnection updates the verdict of specific connection in the kernel extension.
func UpdateVerdictOfConnection(conn *network.Connection) error {
	return windowskext.UpdateVerdict(conn)
}

// GetKextVersion returns the version of the kernel extension.
func GetKextVersion() (string, error) {
	version, err := windowskext.GetVersion()
	if err != nil {
		return "", err
	}

	return version.String(), nil
}
