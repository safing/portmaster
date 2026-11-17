package resolver

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestNameserverConfigChangeHandlerConcurrent checks that the resolvers end up
// loaded with the configured nameservers when config change callbacks run
// concurrently, as they do when the EventMgr runs each callback in its own
// worker.
func TestNameserverConfigChangeHandlerConcurrent(t *testing.T) {
	t.Parallel()

	var (
		configured atomic.Value // string: currently configured nameservers
		loaded     atomic.Value // string: nameservers used by the last reload

		// When set, the next config read is held up until released,
		// simulating a callback that is preempted right after reading.
		blockNextRead atomic.Bool
		blocked       = make(chan struct{})
		release       = make(chan struct{})
	)
	configured.Store("X")
	loaded.Store("X")

	getNameservers := func() []string {
		v, _ := configured.Load().(string)
		if blockNextRead.CompareAndSwap(true, false) {
			close(blocked)
			<-release
		}
		return []string{v}
	}
	reload := func() {
		// loadResolvers reads the current config itself.
		loaded.Store(configured.Load())
	}

	handler := newNameserverConfigChangeHandler(getNameservers, reload)
	call := func() <-chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = handler(nil, struct{}{})
		}()
		return done
	}

	// Change X -> Y: the callback reads Y and is then held up.
	configured.Store("Y")
	blockNextRead.Store(true)
	first := call()
	<-blocked

	// Change Y -> Z while the first callback is still running.
	configured.Store("Z")
	second := call()
	select {
	case <-second:
	case <-time.After(200 * time.Millisecond):
		// The second callback waits for the first one.
	}

	// Let the first callback finish, then wait for both.
	close(release)
	<-first
	<-second

	// Change Z -> Y.
	configured.Store("Y")
	<-call()

	got, _ := loaded.Load().(string)
	want, _ := configured.Load().(string)
	if got != want {
		t.Fatalf("resolvers loaded with nameservers %q, but %q are configured", got, want)
	}
}
