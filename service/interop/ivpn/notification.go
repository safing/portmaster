package ivpn

import (
	"context"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/notifications"
)

func (i *InteropIvpn) showNotificationWarnOldVersion() *notifications.Notification {
	notification := &notifications.Notification{
		EventID:      "interop:ivpn-old-version",
		Type:         notifications.Warning,
		Title:        "IVPN 客户端兼容性提示",
		Message:      `Portmaster 检测到 IVPN 客户端，但已安装的版本在与 Portmaster 同时运行时可能无法完全兼容，部分功能可能无法按预期工作。请考虑将 IVPN 客户端更新到最新版本。`,
		ShowOnSystem: true,
		Expires:      time.Now().Add(5 * time.Minute).Unix(),
		AvailableActions: []*notifications.Action{
			{
				ID:   "ack",
				Text: "确定",
			},
		},
	}
	notifications.Notify(notification)
	return notification
}

func (i *InteropIvpn) initAndShowNotification() *notifications.Notification {
	const actionSuppressID = "suppress"

	notification := &notifications.Notification{
		EventID: "interop:ivpn",
		Type:    notifications.Info,
		Title:   "检测到 IVPN 客户端",
		Message: `Portmaster 检测到 IVPN 客户端，并将允许其 VPN 和服务连接。`,
		AvailableActions: []*notifications.Action{
			{
				ID:   "ack",
				Text: "确定",
			},
			{
				ID:         actionSuppressID,
				Text:       "不再显示",
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
