package process

import (
	"sync"
	"testing"

	"github.com/safing/portmaster/service/profile"
)

// TestPrimaryProfileIDConcurrentReassign checks that IsSystemResolver and
// GetPrimaryProfileID are safe to call while the profile is being (re)assigned
// through setProfile, the write path shared by GetProfile, RefetchProfile and
// GetNetworkHost. RefetchProfile reassigns the profile of a process that is
// shared by all of its connections (network.reAttributeConnection), while the
// firewall calls IsSystemResolver on other connections of the same process
// without holding the process lock.
func TestPrimaryProfileIDConcurrentReassign(t *testing.T) {
	t.Parallel()

	p := &Process{}
	a := &profile.LayeredProfile{}
	b := &profile.LayeredProfile{}
	const otherID = "local/other"

	const rounds = 1000
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range rounds {
			id, lp := systemResolverScopedID, a
			if i%2 == 1 {
				id, lp = otherID, b
			}
			// Same write pattern as GetProfile/RefetchProfile.
			p.Lock()
			p.setProfile(id, lp)
			p.Unlock()
		}
	})

	for range rounds {
		_ = p.IsSystemResolver()
		if id := p.GetPrimaryProfileID(); id != "" && id != systemResolverScopedID && id != otherID {
			t.Errorf("unexpected primary profile ID %q", id)
			break
		}
	}
	wg.Wait()

	// The exported (serialized) field and the accessor must agree.
	p.Lock()
	defer p.Unlock()
	if p.PrimaryProfileID != p.GetPrimaryProfileID() {
		t.Fatalf("field %q and accessor %q disagree", p.PrimaryProfileID, p.GetPrimaryProfileID())
	}
}

// TestPrimaryProfileIDZeroValue checks the accessors on processes without a
// profile and on nil processes.
func TestPrimaryProfileIDZeroValue(t *testing.T) {
	t.Parallel()

	var nilProc *Process
	if nilProc.GetPrimaryProfileID() != "" || nilProc.IsSystemResolver() {
		t.Fatal("nil process must have no primary profile ID")
	}
	p := &Process{}
	if p.GetPrimaryProfileID() != "" || p.IsSystemResolver() {
		t.Fatal("process without profile must have no primary profile ID")
	}
}
