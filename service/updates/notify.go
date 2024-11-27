package updates

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/notifications"
)

const (
	updateFailed            = "updates:failed"
	updateSuccess           = "updates:success"
	updateSuccessPending    = "updates:success-pending"
	updateSuccessDownloaded = "updates:success-downloaded"

	failedUpdateNotifyDurationThreshold = 24 * time.Hour
	failedUpdateNotifyCountThreshold    = 3
)

var updateFailedCnt = new(atomic.Int32)

func (u *Updates) notificationsEnabled() bool {
	return u.instance.Notifications() != nil
}

func notifyUpdateSuccess(force bool) {
	if !module.notificationsEnabled() {
		return
	}

	updateFailedCnt.Store(0)
	module.states.Clear()
	updateState := registry.GetState().Updates

	flavor := updateSuccess
	switch {
	case len(updateState.PendingDownload) > 0:
		// Show notification if there are pending downloads.
		flavor = updateSuccessPending
	case updateState.LastDownloadAt != nil &&
		time.Since(*updateState.LastDownloadAt) < 5*time.Second:
		// Show notification if we downloaded something within the last minute.
		flavor = updateSuccessDownloaded
	case force:
		// Always show notification if update was manually triggered.
	default:
		// Otherwise, the update was uneventful. Do not show notification.
		return
	}

	switch flavor {
	case updateSuccess:
		notifications.Notify(&notifications.Notification{
			EventID: updateSuccess,
			Type:    notifications.Info,
			Title:   "Portmaster Is Up-To-Date",
			Message: "Portmaster successfully checked for updates. Everything is up to date.\n\n" + getUpdatingInfoMsg(),
			Expires: time.Now().Add(1 * time.Minute).Unix(),
			AvailableActions: []*notifications.Action{
				{
					ID:   "ack",
					Text: "OK",
				},
			},
		})

	case updateSuccessPending:
		msg := fmt.Sprintf(
			`%d updates are available for download:

- %s

Press "Download Now" to download and automatically apply all pending updates. You will be notified of important updates that need restarting.`,
			len(updateState.PendingDownload),
			strings.Join(updateState.PendingDownload, "\n- "),
		)

		notifications.Notify(&notifications.Notification{
			EventID: updateSuccess,
			Type:    notifications.Info,
			Title:   fmt.Sprintf("%d Updates Available", len(updateState.PendingDownload)),
			Message: msg,
			AvailableActions: []*notifications.Action{
				{
					ID:   "ack",
					Text: "OK",
				},
				{
					ID:   "download",
					Text: "Download Now",
					Type: notifications.ActionTypeWebhook,
					Payload: &notifications.ActionTypeWebhookPayload{
						URL:          apiPathCheckForUpdates + "?download",
						ResultAction: "display",
					},
				},
			},
		})

	case updateSuccessDownloaded:
		msg := fmt.Sprintf(
			`%d updates were downloaded and applied:

- %s

%s
`,
			len(updateState.LastDownload),
			strings.Join(updateState.LastDownload, "\n- "),
			getUpdatingInfoMsg(),
		)

		notifications.Notify(&notifications.Notification{
			EventID: updateSuccess,
			Type:    notifications.Info,
			Title:   fmt.Sprintf("%d Updates Applied", len(updateState.LastDownload)),
			Message: msg,
			Expires: time.Now().Add(1 * time.Minute).Unix(),
			AvailableActions: []*notifications.Action{
				{
					ID:   "ack",
					Text: "OK",
				},
			},
		})

	}
}

func getUpdatingInfoMsg() string {
	switch {
	case enableSoftwareUpdates() && enableIntelUpdates():
		return "You will be notified of important updates that need restarting."
	case enableIntelUpdates():
		return "Automatic software updates are disabled, but you will be notified when a new software update is ready to be downloaded and applied."
	default:
		return "Automatic software updates are disabled. Please check for updates regularly yourself."
	}
}

func notifyUpdateCheckFailed(force bool, err error) {
	if !module.notificationsEnabled() {
		return
	}

	failedCnt := updateFailedCnt.Add(1)
	lastSuccess := registry.GetState().Updates.LastSuccessAt

	switch {
	case force:
		// Always show notification if update was manually triggered.
	case failedCnt < failedUpdateNotifyCountThreshold:
		// Not failed often enough for notification.
		return
	case lastSuccess == nil:
		// No recorded successful update.
	case time.Now().Add(-failedUpdateNotifyDurationThreshold).Before(*lastSuccess):
		// Failed too recently for notification.
		return
	}

	notifications.NotifyWarn(
		updateFailed,
		"Update Check Failed",
		fmt.Sprintf(
			"Portmaster failed to check for updates. This might be a temporary issue of your device, your network or the update servers. The Portmaster will automatically try again later. The error was: %s",
			err,
		),
		notifications.Action{
			Text: "Try Again Now",
			Type: notifications.ActionTypeWebhook,
			Payload: &notifications.ActionTypeWebhookPayload{
				URL:          apiPathCheckForUpdates,
				ResultAction: "display",
			},
		},
	).SyncWithState(module.states)
}
