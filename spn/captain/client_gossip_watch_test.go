package captain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/spn/hub"
)

// gossipWatchTestStart is a fixed start time, so that the tests do not depend
// on the real clock.
var gossipWatchTestStart = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// newTestGossipWatch returns a watch that was restarted at the given time.
func newTestGossipWatch(start time.Time) *gossipWatch {
	return &gossipWatch{
		lastMessage: start,
		lastCheck:   start,
		avoided:     make(map[string]time.Time),
	}
}

// tickGossipWatch measures the silence in the given steps for the given
// duration, as the client health check does, and returns the time of the last
// measurement.
func tickGossipWatch(w *gossipWatch, from time.Time, duration, step time.Duration) time.Time {
	now := from
	for end := from.Add(duration); now.Before(end); {
		now = now.Add(step)
		w.silence(now)
	}
	return now
}

// TestGossipWatchSilence checks how the silence is measured across health
// check ticks and device sleep.
func TestGossipWatchSilence(t *testing.T) {
	t.Parallel()

	// In sleep mode the health check ticks every 5 minutes. These gaps are
	// below the sleep threshold and must not reset the measurement.
	t.Run("counts sleep mode ticks", func(t *testing.T) {
		t.Parallel()

		w := newTestGossipWatch(gossipWatchTestStart)
		now := tickGossipWatch(w, gossipWatchTestStart, 30*time.Minute, clientHealthCheckTickDurationSleepMode)
		assert.Equal(t, 30*time.Minute, w.silence(now))
	})

	// A gap far above the sleep threshold means the device was asleep. The
	// silence from before the sleep is discarded.
	t.Run("resets after device sleep", func(t *testing.T) {
		t.Parallel()

		// 20 minutes of silence before the sleep.
		w := newTestGossipWatch(gossipWatchTestStart)
		now := tickGossipWatch(w, gossipWatchTestStart, 20*time.Minute, clientHealthCheckTickDuration)

		// Wake-up after 8 hours.
		now = now.Add(8 * time.Hour)
		assert.Equal(t, time.Duration(0), w.silence(now))
		assert.False(t, w.drop(now, "home", gossipWatchRefHubs))

		// The silence is measured from the wake-up on.
		now = tickGossipWatch(w, now, 5*time.Minute, clientHealthCheckTickDuration)
		assert.Equal(t, 5*time.Minute, w.silence(now))
	})
}

// TestGossipWatchDropLimit checks that the home hub is dropped exactly when
// the silence reaches the limit, and that the limit depends on the map size.
func TestGossipWatchDropLimit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		otherHubs int           // other active hubs on the map
		limit     time.Duration // expected silence limit
	}{
		{"reference map size", gossipWatchRefHubs, gossipWatchSilenceLimit},
		{"larger map", 100, gossipWatchSilenceLimit},
		{"half of the reference", 20, 60 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newTestGossipWatch(gossipWatchTestStart)

			// One minute before the limit: no drop.
			now := tickGossipWatch(w, gossipWatchTestStart, tc.limit-time.Minute, clientHealthCheckTickDuration)
			assert.False(t, w.drop(now, "home", tc.otherHubs), "dropped before the limit")
			assert.Empty(t, w.avoided)

			// At the limit: drop, and the hub is avoided.
			now = tickGossipWatch(w, now, time.Minute, clientHealthCheckTickDuration)
			assert.True(t, w.drop(now, "home", tc.otherHubs), "not dropped at the limit")
			assert.Equal(t, now.Add(gossipWatchAvoidFor), w.avoided["home"])
		})
	}

	// Without other hubs there is no gossip to expect, however long the
	// silence lasts.
	t.Run("no other hubs", func(t *testing.T) {
		t.Parallel()

		w := newTestGossipWatch(gossipWatchTestStart)
		now := tickGossipWatch(w, gossipWatchTestStart, 100*time.Hour, clientHealthCheckTickDuration)
		assert.False(t, w.drop(now, "home", 0))
		assert.False(t, w.drop(now, "home", -1))
		assert.Empty(t, w.avoided)
	})
}

// TestGossipWatchDropGuards checks the cases in which the home hub is not
// dropped although the silence has reached the limit.
func TestGossipWatchDropGuards(t *testing.T) {
	t.Parallel()

	// After a drop, the next hub is only dropped once gossip has arrived in
	// between.
	t.Run("one drop until gossip arrives", func(t *testing.T) {
		t.Parallel()

		// The first silent home hub is dropped.
		w := newTestGossipWatch(gossipWatchTestStart)
		now := tickGossipWatch(w, gossipWatchTestStart, gossipWatchSilenceLimit, clientHealthCheckTickDuration)
		assert.True(t, w.drop(now, "first", gossipWatchRefHubs))

		// The next home hub is silent as well.
		now = tickGossipWatch(w, now, 2*time.Hour, clientHealthCheckTickDuration)
		assert.False(t, w.drop(now, "second", gossipWatchRefHubs))
		assert.NotContains(t, w.avoided, "second")

		// Gossip arrives, as recorded by noteMessage.
		w.lastMessage = now
		w.dropped = false

		// After another full limit of silence, the second hub is dropped too.
		now = tickGossipWatch(w, now, gossipWatchSilenceLimit, clientHealthCheckTickDuration)
		assert.True(t, w.drop(now, "second", gossipWatchRefHubs))
		assert.Contains(t, w.avoided, "first")
		assert.Contains(t, w.avoided, "second")
	})

	// Gossip can arrive between the two steps of the check. The drop must
	// then be decided on the new state.
	t.Run("gossip between measurement and drop", func(t *testing.T) {
		t.Parallel()

		w := newTestGossipWatch(gossipWatchTestStart)
		now := tickGossipWatch(w, gossipWatchTestStart, gossipWatchSilenceLimit, clientHealthCheckTickDuration)

		// Gossip arrives after the silence was measured.
		w.lastMessage = now.Add(time.Millisecond)

		assert.False(t, w.drop(now, "home", gossipWatchRefHubs))
		assert.Empty(t, w.avoided)
	})
}

// TestGossipWatchAvoidedLast checks the order in which home hub candidates are
// returned.
func TestGossipWatchAvoidedLast(t *testing.T) {
	t.Parallel()

	candidates := []*hub.Hub{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}

	// ids returns the IDs of the given hubs, in order.
	ids := func(hubs []*hub.Hub) []string {
		list := make([]string, 0, len(hubs))
		for _, h := range hubs {
			list = append(list, h.ID)
		}
		return list
	}

	// Avoided hubs go last. Both groups keep their order.
	t.Run("avoided hubs move to the end", func(t *testing.T) {
		t.Parallel()

		w := newTestGossipWatch(time.Now())
		w.avoided["a"] = time.Now().Add(time.Hour)
		w.avoided["c"] = time.Now().Add(time.Hour)
		assert.Equal(t, []string{"b", "d", "a", "c"}, ids(w.avoidedLast(candidates)))
	})

	// An expired entry no longer changes the order and is removed.
	t.Run("expired entries are removed", func(t *testing.T) {
		t.Parallel()

		w := newTestGossipWatch(time.Now())
		w.avoided["a"] = time.Now().Add(-time.Minute)
		assert.Equal(t, []string{"a", "b", "c", "d"}, ids(w.avoidedLast(candidates)))
		assert.Empty(t, w.avoided)
	})
}
