package updates

import (
	"fmt"
	"time"

	"github.com/Safing/portbase/notifications"
)

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
					Message: fmt.Sprintf("There is an update available for the portmaster core (%s), you may apply the update with the -upgrade flag.", version),
					Type:    notifications.Info,
					Expires: time.Now().Add(1 * time.Minute).Unix(),
				}).Init().Save()

			}
		}

		time.Sleep(1 * time.Hour)
	}
}
