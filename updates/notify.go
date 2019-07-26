package updates

import (
	"fmt"
	"time"

	"github.com/safing/portbase/notifications"
)

const coreIdentifier = "core/portmaster-core"

var lastNotified time.Time

func updateNotifier() {
	time.Sleep(30 * time.Second)
	for {

		_, version, _, ok := getLatestFilePath(coreIdentifier)
		if ok {
			status.Lock()
			liveVersion := status.Core.Version
			status.Unlock()

			if version != liveVersion {

				// create notification
				(&notifications.Notification{
					ID:      "updates-core-update-available",
					Message: fmt.Sprintf("There is an update available for the Portmaster core (v%s), please restart the Portmaster to apply the update.", version),
					Type:    notifications.Info,
					Expires: time.Now().Add(1 * time.Minute).Unix(),
				}).Init().Save()

			}
		}

		time.Sleep(1 * time.Hour)
	}
}
