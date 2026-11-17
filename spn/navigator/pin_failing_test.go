package navigator

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/hub"
)

// TestPinFailingStateLocking checks that the failing state of a Pin, which is
// set from outside of the package without holding the Map lock, is
// synchronized with state updates done while holding the Map lock.
func TestPinFailingStateLocking(t *testing.T) {
	t.Parallel()

	const rounds = 2000

	// Create a minimal map with a single Hub without IPs, which keeps the
	// test fast, as no geoip lookups are needed.
	m := NewMap("Test-Map-Failing-State-Locking", false)
	defer m.Close()
	m.UpdateHub(&hub.Hub{
		ID:        "test-hub",
		Map:       m.Name,
		Info:      &hub.Announcement{ID: "test-hub"},
		Status:    &hub.Status{},
		FirstSeen: time.Now(),
	})
	pin, ok := m.GetPin("test-hub")
	if !ok {
		t.Fatal("failed to add test hub to map")
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Mark and reset the failing state like spn/crew and spn/captain do:
	// without holding the map lock.
	wg.Go(func() {
		defer close(done)

		for range rounds {
			pin.ResetFailingState()
			pin.MarkAsFailingFor(time.Hour)
			_ = pin.GetState()
			_ = pin.IsFailing()
		}
	})

	// Concurrently run map-locked state updates and readers.
	var changedWhileLocked int
	wg.Go(func() {
		matcher := m.DefaultOptions().Matcher(TransitHub, nil)
		for {
			select {
			case <-done:
				return
			default:
			}

			// Read-modify-write of an unrelated state while holding the map lock.
			m.Lock()
			pin.addStates(StateConnectivityIssues)
			before := pin.State
			runtime.Gosched()
			if pin.State != before {
				changedWhileLocked++
			}
			pin.removeStates(StateConnectivityIssues)
			m.Unlock()

			// Expire failing states.
			_ = m.updateFailingStates(nil)

			// Match pins.
			m.RLock()
			_ = matcher(pin)
			m.RUnlock()
		}
	})

	wg.Wait()

	m.RLock()
	defer m.RUnlock()

	if changedWhileLocked > 0 {
		t.Errorf("pin state changed %d times while the map was locked", changedWhileLocked)
	}
	// The last operation marked the pin as failing for an hour.
	if !pin.State.Has(StateFailing) || !time.Now().Before(pin.FailingUntil) {
		t.Errorf("pin lost failing state: states=%s failingUntil=%s", pin.State, pin.FailingUntil)
	}
	if pin.State.Has(StateConnectivityIssues) {
		t.Errorf("pin state update was lost: states=%s", pin.State)
	}
}
