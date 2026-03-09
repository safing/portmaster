package ivpn

import (
	"context"
	"sync"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/notifications"
)

func (i *InteropIvpn) initAndShowNotification() *notifications.Notification {
	const actionSuppressID = "suppress"

	notification := &notifications.Notification{
		EventID: "interop:ivpn",
		Type:    notifications.Info,
		Title:   "IVPN Client detected",
		Message: `Portmaster has detected the IVPN Client and will allow its VPN and service connections.`,
		AvailableActions: []*notifications.Action{
			{
				ID:   "ack",
				Text: "OK",
			},
			{
				ID:         actionSuppressID,
				Text:       "Don't show again",
				Visibility: notifications.ActionVisibilityDetailed,
			},
		},
	}
	notification.SetActionFunction(func(_ context.Context, n *notifications.Notification) error {
		n.Lock()
		actionID := n.SelectedActionID
		n.Unlock()
		if actionID == actionSuppressID {
			if err := suppressNotification(); err != nil {
				return err
			}
		}
		n.Delete()
		return nil
	})
	notifications.Notify(notification)
	return notification
}

// === Notification state persistence ===

// markerRecord is a minimal database record used as a presence-only marker.
type markerRecord struct {
	record.Base
	sync.Mutex
}

var db = database.NewInterface(&database.Options{Local: true, Internal: true})

// Database key used to persist the user's choice to suppress the IVPN detected notification.
const Notification_DB_ID_IvpnDetectSuppressed = "core:notifications/interop/ivpn/suppressed"

// isNotificationSuppressed returns true if the user has chosen to never see the IVPN compat notification.
func isNotificationSuppressed() bool {
	_, err := db.Get(Notification_DB_ID_IvpnDetectSuppressed)
	return err == nil
}

// suppressNotification persists the user's decision to never show the notification again.
func suppressNotification() error {
	m := &markerRecord{}
	m.SetKey(Notification_DB_ID_IvpnDetectSuppressed)
	return db.Put(m)
}
