package updates

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/safing/portbase/notifications"
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

func notifyUpdateSuccess(forced bool) {
	updateFailedCnt.Store(0)
	module.Resolve(updateFailed)
	updateState := registry.GetState().Updates

	flavor := updateSuccess
	switch {
	case len(updateState.PendingDownload) > 0:
		// Show notification if there are pending downloads.
		flavor = updateSuccessPending
	case updateState.LastDownloadAt != nil &&
		time.Since(*updateState.LastDownloadAt) < time.Minute:
		// Show notification if we downloaded something within the last minute.
		flavor = updateSuccessDownloaded
	case forced:
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
			Message: "Portmaster successfully checked for updates. Everything is up to date. Most updates are applied automatically. You will be notified of important updates that need restarting.",
			Expires: time.Now().Add(1 * time.Minute).Unix(),
			AvailableActions: []*notifications.Action{
				{
					ID:   "ack",
					Text: "OK",
				},
			},
		})

	case updateSuccessPending:
		notifications.Notify(&notifications.Notification{
			EventID: updateSuccess,
			Type:    notifications.Info,
			Title:   fmt.Sprintf("%d Updates Available", len(updateState.PendingDownload)),
			Message: fmt.Sprintf(
				`%d updates are available for download. Press "Download Now" or check for updates later to download and automatically apply all pending updates. You will be notified of important updates that need restarting.`,
				len(updateState.PendingDownload),
			),
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
						URL:          apiPathCheckForUpdates,
						ResultAction: "display",
					},
				},
			},
		})

	case updateSuccessDownloaded:
		notifications.Notify(&notifications.Notification{
			EventID: updateSuccess,
			Type:    notifications.Info,
			Title:   fmt.Sprintf("%d Updates Applied", len(updateState.LastDownload)),
			Message: fmt.Sprintf(
				`%d updates were downloaded and applied. You will be notified of important updates that need restarting.`,
				len(updateState.LastDownload),
			),
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

func notifyUpdateCheckFailed(forced bool, err error) {
	failedCnt := updateFailedCnt.Add(1)
	lastSuccess := registry.GetState().Updates.LastSuccessAt

	switch {
	case forced:
		// Always show notification if update was manually triggered.
	case failedCnt < failedUpdateNotifyCountThreshold:
		// Not failed often enough for notification.
		return
	case lastSuccess == nil:
		// No recorded successful udpate.
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
	).AttachToModule(module)
}
