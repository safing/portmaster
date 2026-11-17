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

// TestSetLocalAddrFactoryOnce checks that the factory can be set once, and
// that setting no factory does not count.
func TestSetLocalAddrFactoryOnce(t *testing.T) { //nolint:paralleltest // Replaces the package-level factory.
	prevFactory := localAddrFactory.Load()
	t.Cleanup(func() { localAddrFactory.Store(prevFactory) })
	localAddrFactory.Store(nil)

	first := &net.UDPAddr{Port: 1}
	second := &net.UDPAddr{Port: 2}

	SetLocalAddrFactory(nil)
	if addr := getLocalAddr(networkUDP); addr != nil {
		t.Fatalf("unexpected local address %v without a factory", addr)
	}

	SetLocalAddrFactory(func(string) net.Addr { return first })
	SetLocalAddrFactory(func(string) net.Addr { return second })
	if addr := getLocalAddr(networkUDP); addr != first {
		t.Fatalf("local address is %v, expected the one of the first factory", addr)
	}
}
