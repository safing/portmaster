package netenv

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckOnlineStatus(t *testing.T) {
	t.Parallel()

	checkOnlineStatus(context.Background())
	t.Logf("online status: %s", GetOnlineStatus())
	t.Logf("captive portal: %+v", GetCaptivePortal())
}

// startFakeMonitor runs the online status monitor loop with a fake check that
// takes the given time and counts its runs.
func startFakeMonitor(t *testing.T, checkTime time.Duration) *atomic.Int64 {
	t.Helper()

	var runs atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runOnlineStatusMonitor(ctx, func(context.Context) {
			time.Sleep(checkTime)
			runs.Add(1)
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &runs
}

// TestCheckAndGetOnlineStatusWithoutMonitor makes sure that the function does
// not block if the online status monitor is not running.
func TestCheckAndGetOnlineStatusWithoutMonitor(t *testing.T) { //nolint:paralleltest // Uses the package-level monitor state.
	done := make(chan struct{})
	go func() {
		defer close(done)
		CheckAndGetOnlineStatus()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CheckAndGetOnlineStatus blocks without a running monitor")
	}
}

// TestCheckAndGetOnlineStatusWaitsForNewCheck makes sure that the function
// returns only after an investigation that started after the call completed,
// also while concurrent callers trigger investigations.
func TestCheckAndGetOnlineStatusWaitsForNewCheck(t *testing.T) { //nolint:paralleltest // Uses the package-level monitor state.
	runs := startFakeMonitor(t, 20*time.Millisecond)

	// Wait until the monitor is running and the initial check is done.
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("monitor did not run its first check")
		}
		time.Sleep(time.Millisecond)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				before := runs.Load()
				CheckAndGetOnlineStatus()
				// An investigation that started after the call must be
				// complete, so at least one more run than before.
				if after := runs.Load(); after <= before {
					t.Errorf("returned without a new check: runs before %d, after %d", before, after)
					return
				}
			}
		})
	}
	wg.Wait()
}

var errTestConnectTimeout = errors.New("connect timeout")

// TestQueryDNSTestDomainRetriesOnce checks that a single failed dns query
// check is retried before it is reported, and that a second failure is.
func TestQueryDNSTestDomainRetriesOnce(t *testing.T) { //nolint:paralleltest // Replaces package-level hooks.
	oldQuery, oldDelay := DNSTestQueryFunc, dnsCheckRetryDelay
	t.Cleanup(func() { DNSTestQueryFunc, dnsCheckRetryDelay = oldQuery, oldDelay })
	dnsCheckRetryDelay = time.Millisecond

	var calls int
	failFirst := func(n int) func(context.Context, string) ([]net.IP, bool, error) {
		return func(_ context.Context, _ string) ([]net.IP, bool, error) {
			calls++
			if calls <= n {
				return nil, false, errTestConnectTimeout
			}
			return []net.IP{net.IPv4(1, 1, 1, 1)}, true, nil
		}
	}

	// One failure: the retry succeeds.
	calls = 0
	DNSTestQueryFunc = failFirst(1)
	ips, ok, err := queryDNSTestDomain(context.Background())
	if !ok || err != nil || len(ips) != 1 || calls != 2 {
		t.Errorf("one failure: got ips=%v ok=%v err=%v after %d calls, want success after 2 calls", ips, ok, err, calls)
	}

	// Two failures: the failure is reported.
	calls = 0
	DNSTestQueryFunc = failFirst(2)
	_, ok, err = queryDNSTestDomain(context.Background())
	if ok || err == nil || calls != 2 {
		t.Errorf("two failures: got ok=%v err=%v after %d calls, want failure after 2 calls", ok, err, calls)
	}

	// A successful first query is not repeated.
	calls = 0
	DNSTestQueryFunc = failFirst(0)
	if _, ok, _ = queryDNSTestDomain(context.Background()); !ok || calls != 1 {
		t.Errorf("success: got ok=%v after %d calls, want 1 call", ok, calls)
	}

	// No retry once the context is done.
	calls = 0
	DNSTestQueryFunc = failFirst(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok, _ = queryDNSTestDomain(ctx); ok || calls != 1 {
		t.Errorf("canceled: got ok=%v after %d calls, want failure after 1 call", ok, calls)
	}
}
