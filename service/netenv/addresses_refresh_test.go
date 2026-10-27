package netenv

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errTestRefresh = errors.New("test error")

// stubInterfaceAddrs replaces the OS call of the networks refresh and resets
// the refresh state before and after the test.
func stubInterfaceAddrs(t *testing.T, fn func() ([]net.Addr, error)) {
	t.Helper()

	reset := func(fn func() ([]net.Addr, error)) {
		// Wait for a running refresh, so that the state is not changed under it.
		_, _, _ = myNetworksRefresh.Do("refresh", func() (any, error) { return nil, nil })

		getInterfaceAddrs = fn
		myNetworksAtomic.Store(&[]*net.IPNet{})
		myNetworksDontRefreshUntil = time.Time{}
		myNetworksRefreshError = nil
		myNetworksRefreshFailed.Store(false)
	}
	reset(fn)
	t.Cleanup(func() { reset(osGetInterfaceAddrs) })
}

func TestMyNetworksRefreshIsShared(t *testing.T) { //nolint:paralleltest // Replaces the package-level refresh state.
	newNet := &net.IPNet{IP: net.ParseIP("192.0.2.1").To4(), Mask: net.CIDRMask(24, 32)}
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	stubInterfaceAddrs(t, func() ([]net.Addr, error) {
		calls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-unblock
		return []net.Addr{newNet}, nil
	})
	t.Cleanup(release)

	// Many callers miss while the OS call is slow.
	const callers = 100
	var wg sync.WaitGroup
	results := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			_, err := IsMyIP(net.IPv4(8, 8, 8, 8))
			results <- err
		})
	}

	// Let the callers join the running call, then let it return.
	<-entered
	time.Sleep(100 * time.Millisecond)
	release()
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("caller failed: %s", err)
		}
	}

	// The callers shared the call instead of making one each.
	if n := calls.Load(); n != 1 {
		t.Fatalf("OS call made %d times for %d callers, want 1", n, callers)
	}

	// The result of the shared call is used.
	if mine, err := IsMyIP(newNet.IP); err != nil || !mine {
		t.Fatalf("IP of the refreshed network: mine=%v err=%v", mine, err)
	}
}

func TestMyNetworksRefreshIsThrottledFromEnd(t *testing.T) { //nolint:paralleltest // Replaces the package-level refresh state.
	var calls atomic.Int32
	stubInterfaceAddrs(t, func() ([]net.Addr, error) {
		calls.Add(1)
		time.Sleep(1200 * time.Millisecond)
		return nil, nil
	})

	// A call that takes longer than the throttle time is not followed by the
	// next one at once.
	if err := refreshMyNetworks(); err != nil {
		t.Fatalf("refresh failed: %s", err)
	}
	if err := refreshMyNetworks(); err != nil {
		t.Fatalf("refresh failed: %s", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("OS call made %d times, want 1", n)
	}
}

func TestMyNetworksRefreshErrorIsRetried(t *testing.T) { //nolint:paralleltest // Replaces the package-level refresh state.
	var calls atomic.Int32
	stubInterfaceAddrs(t, func() ([]net.Addr, error) {
		calls.Add(1)
		return nil, errTestRefresh
	})

	if err := refreshMyNetworks(); !errors.Is(err, errTestRefresh) {
		t.Fatalf("got error %v, want %v", err, errTestRefresh)
	}

	// Within the throttle time, the previous error is returned without a new
	// OS call.
	if err := refreshMyNetworks(); !errors.Is(err, errTestRefresh) {
		t.Fatalf("got error %v, want %v", err, errTestRefresh)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("OS call made %d times, want 1", n)
	}

	// A failed refresh is tried again by the next call.
	if !myNetworksChanged() {
		t.Fatal("failed refresh does not trigger another refresh")
	}
}

func TestMyNetworksChangeDuringRefreshIsKept(t *testing.T) { //nolint:paralleltest // Replaces the package-level refresh state.
	entered := make(chan struct{})
	unblock := make(chan struct{})
	stubInterfaceAddrs(t, func() ([]net.Addr, error) {
		close(entered)
		<-unblock
		return nil, nil
	})

	done := make(chan error, 1)
	go func() { done <- refreshMyNetworks() }()

	// The network changes while the OS call runs.
	<-entered
	networkChangedBroadcastFlag.NotifyAndReset()
	close(unblock)
	if err := <-done; err != nil {
		t.Fatalf("refresh failed: %s", err)
	}

	if !myNetworksChanged() {
		t.Fatal("network change during the refresh was lost")
	}
}

func TestMyNetworksCallersWaitForRefreshAfterChange(t *testing.T) { //nolint:paralleltest // Replaces the package-level refresh state.
	newNet := &net.IPNet{IP: net.ParseIP("192.0.2.1").To4(), Mask: net.CIDRMask(24, 32)}
	entered := make(chan struct{})
	enter := sync.OnceFunc(func() { close(entered) })
	unblock := make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	stubInterfaceAddrs(t, func() ([]net.Addr, error) {
		enter()
		<-unblock
		return []net.Addr{newNet}, nil
	})
	t.Cleanup(release)

	// The network changes and a refresh starts.
	networkChangedBroadcastFlag.NotifyAndReset()
	done := make(chan error, 1)
	go func() { done <- refreshMyNetworks() }()
	<-entered

	// A caller during that refresh waits for the new networks instead of
	// using the old ones.
	result := make(chan *net.IPNet, 1)
	go func() {
		localNet, _ := GetLocalNetwork(newNet.IP)
		result <- localNet
	}()
	select {
	case <-result:
		t.Fatal("caller did not wait for the refresh after the network change")
	case <-time.After(100 * time.Millisecond):
	}

	release()
	if localNet := <-result; localNet == nil {
		t.Fatal("caller did not get the new networks")
	}
	if err := <-done; err != nil {
		t.Fatalf("refresh failed: %s", err)
	}
}
