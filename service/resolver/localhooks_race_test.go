package resolver

import (
	"net"
	"sync"
	"testing"
)

// TestLocalHooksSwapIsRaceFree swaps the package-level hooks while a
// goroutine, like a still running resolver query, keeps reading them.
func TestLocalHooksSwapIsRaceFree(t *testing.T) { //nolint:paralleltest // Replaces the package-level hooks.
	prevHooks := localPortHooks.Load()
	prevFactory := localAddrFactory.Load()
	t.Cleanup(func() {
		localPortHooks.Store(prevHooks)
		localAddrFactory.Store(prevFactory)
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			authorizeLocalPort(networkUDP, 1)
			releaseLocalPort(networkUDP, 1, false)
			getLocalAddr(networkUDP)
		}
	})

	for range 100000 {
		localPortHooks.Store(&LocalPortHooks{Authorize: func(string, uint16) {}})
		factory := func(string) net.Addr { return nil }
		localAddrFactory.Store(&factory)
	}
	close(stop)
	wg.Wait()
}
