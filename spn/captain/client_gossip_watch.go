package captain

import (
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/navigator"
)

// Home hub gossip watch.
//
// A client receives all map updates ("gossip") through its home hub. A home
// hub that answers pings but relays no gossip passes the ping check, while the
// client's map goes stale and the other hubs expire from it.
//
// The watch moves the client to another home hub in that case:
//   - GossipOp reports new gossip via noteMessage.
//   - clientCheckHomeHubGossip requests a reconnect after
//     gossipWatchSilenceLimit without gossip about other hubs.
//   - establishHomeHub tries the dropped hub last, see avoidedLast.
//
// Safeguards against dropping a healthy hub:
//   - Time asleep is not counted.
//   - Only one hub is dropped until gossip arrives again.
//   - A dropped hub is not marked as failing and remains a fallback.
//
// Active in Portmaster only (conf.Integrated). Hubs and the observation hub
// are unaffected.

const (
	// gossipWatchSilenceLimit is the maximum time without gossip about other
	// hubs before the home hub is dropped. It applies to maps with at least
	// gossipWatchRefHubs other active hubs and is longer for smaller maps.
	gossipWatchSilenceLimit = 30 * time.Minute

	// gossipWatchRefHubs is the number of other active hubs that
	// gossipWatchSilenceLimit is calibrated for.
	gossipWatchRefHubs = 40

	// gossipWatchAvoidFor is how long a dropped hub is tried last as home hub.
	gossipWatchAvoidFor = 12 * time.Hour
)

// homeHubGossipWatch watches the gossip relayed by the client's home hub.
var homeHubGossipWatch = &gossipWatch{avoided: make(map[string]time.Time)}

// gossipWatch detects a home hub that has stopped relaying gossip. It records
// when gossip about other hubs last arrived, and which home hubs were dropped
// for staying silent too long. It is safe for concurrent use.
type gossipWatch struct {
	lock sync.Mutex

	// lastMessage is the time of the last gossip about another hub, or of the
	// last restart.
	lastMessage time.Time

	// lastCheck is the time of the last silence measurement, or of the last
	// restart. It is used to detect device sleep.
	lastCheck time.Time

	// dropped is set by a drop and cleared by the next gossip. While set, no
	// further hub is dropped: if the next home hub is silent too, the cause is
	// likely elsewhere.
	dropped bool

	// avoided maps dropped hubs to the time until which they are tried last.
	avoided map[string]time.Time
}

// noteMessage records gossip about the given hub. It is a no-op outside
// Portmaster.
func (w *gossipWatch) noteMessage(hubID string) {
	if !conf.Integrated() {
		return
	}

	// The home hub's own status does not show that it still relays.
	home, _ := navigator.Main.GetHome()
	if home != nil && home.Hub.ID == hubID {
		return
	}

	w.lock.Lock()
	defer w.lock.Unlock()

	w.lastMessage = time.Now()
	w.dropped = false
}

// restart resets the watch. It is called after connecting to a home hub.
func (w *gossipWatch) restart() {
	w.lock.Lock()
	defer w.lock.Unlock()

	now := time.Now()
	w.lastMessage = now
	w.lastCheck = now
}

// silence returns how long no gossip about other hubs has arrived. A long gap
// since the previous call indicates device sleep and resets the measurement.
func (w *gossipWatch) silence(now time.Time) time.Duration {
	// Same threshold as in clientCheckHomeHubConnection.
	sleepSuspectedAfter := clientHealthCheckTickDuration +
		clientHealthCheckTickDurationSleepMode +
		(clientHealthCheckTimeout * 2)

	w.lock.Lock()
	defer w.lock.Unlock()

	if now.Sub(w.lastCheck) > sleepSuspectedAfter {
		w.lastMessage = now
	}
	w.lastCheck = now

	return now.Sub(w.lastMessage)
}

// drop reports whether the home hub must be dropped for its silence and, if
// so, marks it as avoided. Call it after silence, with the same time.
//
// Fewer hubs produce less gossip, so below gossipWatchRefHubs other active
// hubs the limit grows in inverse proportion to their number. Without other
// hubs, no hub is dropped.
func (w *gossipWatch) drop(now time.Time, homeID string, otherHubs int) bool {
	var limit time.Duration
	switch {
	case otherHubs <= 0:
		return false
	case otherHubs >= gossipWatchRefHubs:
		limit = gossipWatchSilenceLimit
	default:
		limit = gossipWatchSilenceLimit * gossipWatchRefHubs / time.Duration(otherHubs)
	}

	w.lock.Lock()
	defer w.lock.Unlock()

	if w.dropped || now.Sub(w.lastMessage) < limit {
		return false
	}

	w.dropped = true
	w.avoided[homeID] = now.Add(gossipWatchAvoidFor)
	return true
}

// avoidedLast moves recently dropped hubs to the end of the given home hub
// candidates. The order is otherwise preserved.
func (w *gossipWatch) avoidedLast(candidates []*hub.Hub) []*hub.Hub {
	w.lock.Lock()
	defer w.lock.Unlock()

	// Remove expired entries.
	now := time.Now()
	for hubID, until := range w.avoided {
		if now.After(until) {
			delete(w.avoided, hubID)
		}
	}
	if len(w.avoided) == 0 {
		return candidates
	}

	others := make([]*hub.Hub, 0, len(candidates))
	var avoided []*hub.Hub
	for _, candidate := range candidates {
		if _, ok := w.avoided[candidate.ID]; ok {
			avoided = append(avoided, candidate)
		} else {
			others = append(others, candidate)
		}
	}
	return append(others, avoided...)
}

// clientCheckHomeHubGossip requests a reconnect if the home hub has relayed no
// gossip about other hubs for too long. It is a no-op outside Portmaster.
func clientCheckHomeHubGossip(_ *mgr.WorkerCtx) clientComponentResult {
	if !conf.Integrated() {
		return clientResultOk
	}

	// A missing home hub is handled by clientCheckHomeHubConnection.
	home, _ := navigator.Main.GetHome()
	if home == nil {
		return clientResultOk
	}

	// Below the base limit, the map size does not matter.
	now := time.Now()
	silentFor := homeHubGossipWatch.silence(now)
	if silentFor < gossipWatchSilenceLimit {
		return clientResultOk
	}

	// Exclude the home hub itself.
	otherHubs := navigator.Main.Stats().States[navigator.StateActive] - 1

	if !homeHubGossipWatch.drop(now, home.Hub.ID, otherHubs) {
		return clientResultOk
	}

	log.Warningf(
		"spn/captain: home hub %s delivered no updates about %d other hubs for %s, connecting to another hub",
		home.Hub, otherHubs, silentFor.Round(time.Minute),
	)

	// Failing states may have been caused by the home hub.
	navigator.Main.ResetFailingStates()

	return clientResultReconnect
}
