package process

import (
	"sync"
	"testing"

	"github.com/safing/portmaster/service/profile"
)

// TestProfileConcurrentReassign checks that Profile() is safe to call while
// the profile is being (re)assigned, as RefetchProfile does when connections
// are re-attributed (network.reAttributeConnection) while other connections of
// the same process read the profile, and as GetProfile does for a process that
// is already stored and reachable via All().
func TestProfileConcurrentReassign(t *testing.T) {
	t.Parallel()

	p := &Process{}
	a := &profile.LayeredProfile{}
	b := &profile.LayeredProfile{}

	const rounds = 1000
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range rounds {
			lp := a
			if i%2 == 1 {
				lp = b
			}
			// Same write pattern as GetProfile/RefetchProfile.
			p.Lock()
			p.setProfile("local/test", lp)
			p.Unlock()
		}
	})

	for range rounds {
		if lp := p.Profile(); lp != nil && lp != a && lp != b {
			t.Fatal("unexpected profile")
		}
	}
	wg.Wait()
}
