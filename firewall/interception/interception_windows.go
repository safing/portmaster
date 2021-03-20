package interception

import (
	"fmt"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portbase/utils/osdetail"
	"github.com/safing/portmaster/firewall/interception/windowskext"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/updates"
)

// start starts the interception.
func start(ch chan packet.Packet) error {
	dllFile, err := updates.GetPlatformFile("kext/portmaster-kext.dll")
	if err != nil {
		return fmt.Errorf("interception: could not get kext dll: %s", err)
	}
	kextFile, err := updates.GetPlatformFile("kext/portmaster-kext.sys")
	if err != nil {
		return fmt.Errorf("interception: could not get kext sys: %s", err)
	}

	err = windowskext.Init(dllFile.Path(), kextFile.Path())
	if err != nil {
		return fmt.Errorf("interception: could not init windows kext: %s", err)
	}

	err = windowskext.Start()
	if err != nil {
		return fmt.Errorf("interception: could not start windows kext: %s", err)
	}

	go windowskext.Handler(ch)
	go checkWindowsDNSCache()

	return nil
}

// stop starts the interception.
func stop() error {
	return windowskext.Stop()
}

func checkWindowsDNSCache() {
	status, err := osdetail.GetServiceStatus("dnscache")
	if err != nil {
		log.Warningf("firewall/interception: failed to check status of Windows DNS-Client: %s", err)
	}

	if status == osdetail.StatusStopped {
		err := osdetail.EnableDNSCache()
		if err != nil {
			log.Warningf("firewall/interception: failed to enable Windows Service \"DNS Client\" (dnscache): %s", err)
		} else {
			log.Warningf("firewall/interception: successfully enabled the dnscache")
			notifyRebootRequired()
		}
	}
}

func notifyRebootRequired() {
	(&notifications.Notification{
		EventID: "interception:windows-dnscache-reboot-required",
		Message: "Please restart your system to complete Portmaster integration.",
		Type:    notifications.Warning,
	}).Save()
}
