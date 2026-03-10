package resolver

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

var (
	slowQueriesSensorCnt atomic.Int64
	slowQueriesSensorSum atomic.Int64
)

// reportRequestDuration reports successful query request duration.
func reportRequestDuration(started time.Time, resolver *Resolver) {
	// TODO: Record prometheus metrics for all resolvers separately.

	// Add query times from system and configured resolvers to slow queries sensor.
	switch resolver.Info.Source {
	case ServerSourceConfigured, ServerSourceOperatingSystem:
		slowQueriesSensorCnt.Add(1)
		slowQueriesSensorSum.Add(int64(time.Since(started)))
	default:
	}
}

// getSlowQueriesSensorValue returns the current avg query time recorded by the
// slow queries sensor.
func getSlowQueriesSensorValue() (avgQueryTime time.Duration) {
	// Get values and check them.
	sum := slowQueriesSensorSum.Load()
	cnt := slowQueriesSensorCnt.Load()
	if cnt < 1 {
		cnt = 1
	}

	return time.Duration(sum / cnt)
}

// resetSlowQueriesSensorValue reset the slow queries sensor values.
func resetSlowQueriesSensorValue() {
	slowQueriesSensorCnt.Store(0)
	slowQueriesSensorSum.Store(0)
}

var suggestUsingStaleCacheNotification *notifications.Notification
var isFirstNotification = true

func suggestUsingStaleCacheTask(_ *mgr.WorkerCtx) error {
	scheduleNextCall := true
	switch {
	case useStaleCache() || useStaleCacheConfigOption.IsSetByUser() || isNotificationSuppressed():
		// If setting is already active, disable task repeating.
		scheduleNextCall = false

		// Delete local reference, if used.
		if suggestUsingStaleCacheNotification != nil {
			suggestUsingStaleCacheNotification.Delete()
			suggestUsingStaleCacheNotification = nil
		}

	case suggestUsingStaleCacheNotification != nil:
		// Check if notification is already active.

		suggestUsingStaleCacheNotification.Lock()
		defer suggestUsingStaleCacheNotification.Unlock()
		if suggestUsingStaleCacheNotification.Meta().IsDeleted() {
			// Reset local reference if notification was deleted.
			suggestUsingStaleCacheNotification = nil
		}

	case getSlowQueriesSensorValue() > 100*time.Millisecond:
		log.Warningf(
			"resolver: suggesting user to use stale dns cache with avg query time of %s for config and system resolvers",
			getSlowQueriesSensorValue().Round(time.Millisecond),
		)

		const actionSuppressID = "suppress"

		// Notify user.
		suggestUsingStaleCacheNotification = &notifications.Notification{
			EventID:      "resolver:suggest-using-stale-cache",
			Type:         notifications.Info,
			Title:        "Speed Up Website Loading",
			Message:      "Portmaster has detected that websites may load slower because DNS queries are currently slower than expected. You may want to switch your DNS provider or enable using expired DNS cache entries for better performance.",
			ShowOnSystem: isFirstNotification && getSlowQueriesSensorValue() > 500*time.Millisecond,
			Expires:      time.Now().Add(10 * time.Minute).Unix(),
			AvailableActions: []*notifications.Action{
				{
					Text: "Open Setting",
					Type: notifications.ActionTypeOpenSetting,
					Payload: &notifications.ActionTypeOpenSettingPayload{
						Key: CfgOptionUseStaleCacheKey,
					},
					Visibility: notifications.ActionVisibilityInAppOnly,
				},
				{
					ID:         actionSuppressID,
					Text:       "Don't show again",
					Visibility: notifications.ActionVisibilityDetailed,
				},
				{
					ID:   "ack",
					Text: "Got it!",
				},
			},
		}
		// Only show the notification on the system for the first time,
		// and do not bother user with multiple system notifications
		isFirstNotification = false

		suggestUsingStaleCacheNotification.SetActionFunction(func(_ context.Context, n *notifications.Notification) error {
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

		notifications.Notify(suggestUsingStaleCacheNotification)
	}

	if scheduleNextCall {
		_ = module.suggestUsingStaleCacheTask.Delay(2 * time.Minute)
	}
	resetSlowQueriesSensorValue()
	return nil
}

// === Notification state persistence ===

// markerRecord is a minimal database record used as a presence-only marker.
type markerRecord struct {
	record.Base
	sync.Mutex
}

var db = database.NewInterface(&database.Options{Local: true, Internal: true})

// Database key used to persist the user's choice to suppress the stale cache notification.
const Notification_DB_ID_StaleCacheSuppressed = "core:notifications/resolver/StaleCache/suppressed"

// isNotificationSuppressed returns true if the user has chosen to never see the stale cache notification.
func isNotificationSuppressed() bool {
	_, err := db.Get(Notification_DB_ID_StaleCacheSuppressed)
	return err == nil
}

// suppressNotification persists the user's decision to never show the notification again.
func suppressNotification() error {
	m := &markerRecord{}
	m.SetKey(Notification_DB_ID_StaleCacheSuppressed)
	return db.Put(m)
}
