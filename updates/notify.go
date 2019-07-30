package updates

import (
	"fmt"
	"time"

	"github.com/safing/portbase/notifications"
)

const coreIdentifier = "core/portmaster-core"

var lastNotified time.Time

func updateNotifier() {
	time.Sleep(5 * time.Minute)
	for {
		ident := coreIdentifier
		if isWindows {
			ident += ".exe"
		}

		file, err := GetLocalPlatformFile(ident)
		if err == nil {
			status.Lock()
			liveVersion := status.Core.Version
			status.Unlock()

			if file.Version() != liveVersion {

				// create notification
				(&notifications.Notification{
					ID:      "updates-core-update-available",
					Message: fmt.Sprintf("There is an update available for the Portmaster core (v%s), please restart the Portmaster to apply the update.", file.Version()),
					Type:    notifications.Info,
					Expires: time.Now().Add(1 * time.Minute).Unix(),
				}).Init().Save()

			}
		}

		time.Sleep(1 * time.Hour)
	}
}
