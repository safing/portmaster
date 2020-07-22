package interception

import (
	"fmt"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portbase/utils/osdetail"
	"github.com/safing/portmaster/firewall/interception/windowskext"
	"github.com/safing/portmaster/updates"
)

// start starts the interception.
func start() error {
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

	go windowskext.Handler(Packets)
	go handleWindowsDNSCache()

	return nil
}

// stop starts the interception.
func stop() error {
	return windowskext.Stop()
}

func handleWindowsDNSCache() {

	err := osdetail.StopService("dnscache")
	if err != nil {
		// cannot stop dnscache, try disabling
		if err == osdetail.ErrServiceNotStoppable {
			err := osdetail.DisableDNSCache()
			if err != nil {
				log.Warningf("firewall/interception: failed to disable Windows Service \"DNS Client\" (dnscache) for better interception: %s", err)
				notifyDisableDNSCache()
			}
			notifyRebootRequired()
			return
		}

		// error while stopping service
		log.Warningf("firewall/interception: failed to stop Windows Service \"DNS Client\" (dnscache) for better interception: %s", err)
		notifyDisableDNSCache()
	}

	// log that service is stopped
	log.Info("firewall/interception: Windows Service \"DNS Client\" (dnscache) is stopped for better interception")

}

func notifyDisableDNSCache() {
	(&notifications.Notification{
		ID:      "windows-disable-dns-cache",
		Message: "The Portmaster needs the Windows Service \"DNS Client\" (dnscache) to be disabled for best effectiveness.",
		Type:    notifications.Warning,
	}).Save()
}

func notifyRebootRequired() {
	(&notifications.Notification{
		ID:      "windows-dnscache-reboot-required",
		Message: "Please restart your system to complete Portmaster integration.",
		Type:    notifications.Warning,
	}).Save()
}
