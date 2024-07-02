package resolver

import (
	"sync/atomic"
	"time"

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

func suggestUsingStaleCacheTask(_ *mgr.WorkerCtx) error {
	scheduleNextCall := true
	switch {
	case useStaleCache() || useStaleCacheConfigOption.IsSetByUser():
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

		// Notify user.
		suggestUsingStaleCacheNotification = &notifications.Notification{
			EventID:      "resolver:suggest-using-stale-cache",
			Type:         notifications.Info,
			Title:        "Speed Up Website Loading",
			Message:      "Portmaster has detected that websites may load slower because DNS queries are currently slower than expected. You may want to switch your DNS provider or enable using expired DNS cache entries for better performance.",
			ShowOnSystem: getSlowQueriesSensorValue() > 500*time.Millisecond,
			Expires:      time.Now().Add(10 * time.Minute).Unix(),
			AvailableActions: []*notifications.Action{
				{
					Text: "Open Setting",
					Type: notifications.ActionTypeOpenSetting,
					Payload: &notifications.ActionTypeOpenSettingPayload{
						Key: CfgOptionUseStaleCacheKey,
					},
				},
				{
					ID:   "ack",
					Text: "Got it!",
				},
			},
		}
		notifications.Notify(suggestUsingStaleCacheNotification)
	}

	if scheduleNextCall {
		_ = module.suggestUsingStaleCacheTask.Delay(2 * time.Minute)
	}
	resetSlowQueriesSensorValue()
	return nil
}
