package network

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
)

func TestVerdictStatePacking(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictUndecided, VerdictUndeterminable, VerdictAccept, VerdictBlock,
		VerdictDrop, VerdictRerouteToNameserver, VerdictRerouteToTunnel,
		VerdictFailed, VerdictRerouteToSplitTun, Verdict(-1), Verdict(math.MaxInt8), Verdict(math.MinInt8),
	}
	pids := []int{
		0, 1, 4, 4242, 4194304, // Linux PID_MAX_LIMIT
		math.MaxInt32, math.MaxUint32, // Windows PIDs are 32 bit.
		-1, -2, -3, -255, math.MinInt32, // Special PIDs are negative.
	}

	for _, verdict := range verdicts {
		for _, pid := range pids {
			for _, permanent := range []bool{false, true} {
				for _, inbound := range []bool{false, true} {
					want := VerdictState{
						Verdict:          verdict,
						VerdictPermanent: permanent,
						Inbound:          inbound,
						PID:              pid,
					}
					packed := packVerdictState(verdict, permanent, inbound, pid)
					if packed&verdictStatePermanentPending != 0 {
						t.Errorf("%+v: pending flag must not be set by packing", want)
					}
					got := unpackVerdictState(packed)
					if got != want {
						t.Errorf("got %+v, want %+v", got, want)
					}
					// The pending flag does not influence any value.
					got = unpackVerdictState(packed | verdictStatePermanentPending)
					if got != want {
						t.Errorf("with pending flag: got %+v, want %+v", got, want)
					}
				}
			}
		}
	}
}

func TestVerdictStatePublishing(t *testing.T) {
	t.Parallel()

	conn := &Connection{
		Verdict: VerdictAccept,
		Inbound: true,
		PID:     -1,
	}

	// Nothing is published before the connection is unlocked the first time.
	if state := conn.VerdictState(); state != (VerdictState{}) {
		t.Errorf("unexpected initial state: %+v", state)
	}

	// Changes are published when unlocking, not before.
	conn.Lock()
	conn.Verdict = VerdictBlock
	if state := conn.VerdictState(); state != (VerdictState{}) {
		t.Errorf("state published within critical section: %+v", state)
	}
	conn.Unlock()
	want := VerdictState{Verdict: VerdictBlock, Inbound: true, PID: -1}
	if state := conn.VerdictState(); state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}

	// TryLock publishes too.
	if !conn.TryLock() {
		t.Fatal("failed to lock connection")
	}
	if conn.TryLock() {
		t.Fatal("locked connection twice")
	}
	conn.PID = 1234
	conn.Inbound = false
	conn.Unlock()
	want = VerdictState{Verdict: VerdictBlock, PID: 1234}
	if state := conn.VerdictState(); state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}

	// Stopping the firewall handler publishes within the critical section.
	conn.Lock()
	conn.Verdict = VerdictDrop
	conn.StopFirewallHandler()
	want.Verdict = VerdictDrop
	if state := conn.VerdictState(); state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}
	conn.Unlock()

	// A permanent verdict set without the lock is visible immediately and is
	// adopted by the connection when it is locked.
	want.VerdictPermanent = true
	if state := conn.MakeVerdictPermanentLockFree(); state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}
	if state := conn.VerdictState(); state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}
	conn.Lock()
	if !conn.VerdictPermanent {
		t.Error("permanent verdict not adopted when locking")
	}
	conn.Unlock()
	if conn.verdictState.Load()&verdictStatePermanentPending != 0 {
		t.Error("pending flag not cleared when unlocking")
	}
	if state := conn.VerdictState(); state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}

	// A permanent verdict that is set while the connection is locked is
	// adopted when unlocking and does not revert other changes.
	conn = &Connection{}
	conn.Lock()
	conn.Verdict = VerdictAccept
	conn.MakeVerdictPermanentLockFree()
	conn.Unlock()
	want = VerdictState{Verdict: VerdictAccept, VerdictPermanent: true}
	if state := conn.VerdictState(); state != want {
		t.Errorf("got %+v, want %+v", state, want)
	}
	if !conn.VerdictPermanent {
		t.Error("permanent verdict not adopted when unlocking")
	}
}

// TestVerdictStateConcurrency checks that the lock-free accessors do not race
// with users of the connection lock and that a permanent verdict is never lost.
func TestVerdictStateConcurrency(t *testing.T) {
	t.Parallel()

	const rounds = 5000
	conn := &Connection{}

	var (
		wg        sync.WaitGroup
		permanent atomic.Bool
	)

	// Lock-free packet path.
	wg.Go(func() {
		for i := range rounds {
			state := conn.VerdictState()
			if permanent.Load() && !state.VerdictPermanent {
				t.Error("permanent verdict was lost")
				return
			}
			if state.PID != int(state.Verdict) {
				t.Errorf("inconsistent snapshot: %+v", state)
				return
			}
			if i == rounds/2 {
				conn.MakeVerdictPermanentLockFree()
				permanent.Store(true)
			}
		}
	})

	// Users of the connection lock.
	for range 2 {
		wg.Go(func() {
			for i := range rounds {
				conn.Lock()
				// Keep Verdict and PID in sync to detect torn snapshots.
				conn.Verdict = Verdict(i % 8)
				conn.PID = i % 8
				conn.Inbound = i%2 == 0
				conn.Unlock()
			}
		})
	}

	wg.Wait()

	conn.Lock()
	defer conn.Unlock()
	if !conn.VerdictPermanent {
		t.Error("connection did not adopt the permanent verdict")
	}
}

func BenchmarkConnectionLockUnlock(b *testing.B) {
	conn := &Connection{Verdict: VerdictAccept, PID: 4242}

	b.Run("Mutex", func(b *testing.B) {
		for b.Loop() {
			conn.Mutex.Lock()
			conn.Mutex.Unlock() //nolint:staticcheck // Empty critical section is intended.
		}
	})

	b.Run("Connection", func(b *testing.B) {
		for b.Loop() {
			conn.Lock()
			conn.Unlock() //nolint:staticcheck // Empty critical section is intended.
		}
	})

	b.Run("VerdictState", func(b *testing.B) {
		var state VerdictState
		for b.Loop() {
			state = conn.VerdictState()
		}
		_ = state
	})
}
