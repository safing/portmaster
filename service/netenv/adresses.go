package netenv

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/netutils"
)

// GetAssignedAddresses returns the assigned IPv4 and IPv6 addresses of the host.
func GetAssignedAddresses() (ipv4 []net.IP, ipv6 []net.IP, err error) {
	addrs, err := osGetInterfaceAddrs()
	if err != nil {
		return nil, nil, err
	}
	for _, addr := range addrs {
		netAddr, ok := addr.(*net.IPNet)
		if !ok {
			log.Warningf("netenv: interface address of unexpected type %T", addr)
			continue
		}

		if ip4 := netAddr.IP.To4(); ip4 != nil {
			ipv4 = append(ipv4, ip4)
		} else {
			ipv6 = append(ipv6, netAddr.IP)
		}
	}
	return
}

// GetAssignedGlobalAddresses returns the assigned global IPv4 and IPv6 addresses of the host.
func GetAssignedGlobalAddresses() (ipv4 []net.IP, ipv6 []net.IP, err error) {
	allv4, allv6, err := GetAssignedAddresses()
	if err != nil {
		return nil, nil, err
	}
	for _, ip4 := range allv4 {
		if netutils.GetIPScope(ip4).IsGlobal() {
			ipv4 = append(ipv4, ip4)
		}
	}
	for _, ip6 := range allv6 {
		if netutils.GetIPScope(ip6).IsGlobal() {
			ipv6 = append(ipv6, ip6)
		}
	}
	return
}

var (
	// myNetworksAtomic holds the host's networks. A refresh replaces the slice
	// and never modifies it, so readers need no lock.
	myNetworksAtomic atomic.Pointer[[]*net.IPNet]

	// myNetworksRefresh runs one refresh at a time; concurrent callers share it.
	myNetworksRefresh singleflight.Group

	// myNetworksRefreshError and myNetworksDontRefreshUntil are only accessed by
	// doRefreshMyNetworks.
	myNetworksRefreshError     error //nolint:errname // Not what the linter thinks this is for.
	myNetworksDontRefreshUntil time.Time

	// myNetworksRefreshFailed is set while the last refresh failed, so that
	// the next call tries again.
	myNetworksRefreshFailed atomic.Bool

	// myNetworksUpdating is set while a refresh after a network change runs, so
	// that callers wait for the new networks.
	myNetworksUpdating atomic.Bool

	// myNetworksNetworkChangedFlag is not concurrency safe and is guarded by
	// myNetworksChangedFlagLock.
	myNetworksNetworkChangedFlag = GetNetworkChangedFlag()
	myNetworksChangedFlagLock    sync.Mutex

	// getInterfaceAddrs reads the interface addresses. Replaced in tests.
	getInterfaceAddrs = osGetInterfaceAddrs
)

func init() {
	myNetworksAtomic.Store(&[]*net.IPNet{})
}

// refreshMyNetworks refreshes myNetworksAtomic. Callers that arrive during a
// refresh wait for it and share its result instead of each making the OS
// call, which can take seconds, e.g. after a resume from sleep.
func refreshMyNetworks() error {
	_, err, _ := myNetworksRefresh.Do("refresh", func() (any, error) {
		return nil, doRefreshMyNetworks()
	})
	return err
}

// doRefreshMyNetworks does the refresh. Only refreshMyNetworks calls it, one
// call at a time.
func doRefreshMyNetworks() error {
	// Check if we already refreshed recently.
	if time.Now().Before(myNetworksDontRefreshUntil) {
		// Return previous error, if available.
		if myNetworksRefreshError != nil {
			return fmt.Errorf("failed to previously refresh interface addresses: %w", myNetworksRefreshError)
		}
		return nil
	}

	// Reset the changed flag before the OS call, so that a change during the
	// call triggers another refresh. Set myNetworksUpdating in the same locked
	// section, so that callers always see one of the two.
	myNetworksChangedFlagLock.Lock()
	changed := myNetworksNetworkChangedFlag.IsSet()
	if changed {
		myNetworksUpdating.Store(true)
	}
	myNetworksNetworkChangedFlag.Refresh()
	myNetworksChangedFlagLock.Unlock()
	if changed {
		defer myNetworksUpdating.Store(false)
	}

	// Refresh assigned networks.
	interfaceNetworks, err := getInterfaceAddrs()

	// Throttle from the end of the call, so that slow calls do not run back to back.
	myNetworksDontRefreshUntil = time.Now().Add(1 * time.Second)
	myNetworksRefreshError = err
	myNetworksRefreshFailed.Store(err != nil)
	if err != nil {
		return fmt.Errorf("failed to refresh interface addresses: %w", err)
	}

	myNetworks := make([]*net.IPNet, 0, len(interfaceNetworks))
	for _, ifNet := range interfaceNetworks {
		ipNet, ok := ifNet.(*net.IPNet)
		if !ok {
			log.Warningf("netenv: interface network of unexpected type %T", ifNet)
			continue
		}

		myNetworks = append(myNetworks, ipNet)
	}
	myNetworksAtomic.Store(&myNetworks)

	return nil
}

// myNetworksChanged returns whether the networks must be refreshed: the
// network changed since the last refresh, the refresh after a change is still
// running, or the last refresh failed.
func myNetworksChanged() bool {
	if myNetworksRefreshFailed.Load() {
		return true
	}

	myNetworksChangedFlagLock.Lock()
	defer myNetworksChangedFlagLock.Unlock()

	return myNetworksNetworkChangedFlag.IsSet() || myNetworksUpdating.Load()
}

// IsMyIP returns whether the given unicast IP is currently configured on the local host.
// Broadcast or multicast addresses will never match, even if valid and in use.
// Function is optimized with the assumption that is likely that the IP is mine.
func IsMyIP(ip net.IP) (yes bool, err error) {
	// Check for IPs that don't need extra checks.
	switch netutils.GetIPScope(ip) { //nolint:exhaustive // Only looking for specific values.
	case netutils.HostLocal:
		return true, nil
	case netutils.LocalMulticast, netutils.GlobalMulticast:
		return false, nil
	}

	// Check if the network changed.
	if myNetworksChanged() {
		err := refreshMyNetworks()
		if err != nil {
			return false, err
		}
	}
	myNetworks := *myNetworksAtomic.Load()

	// Check against assigned IPs.
	for _, myNet := range myNetworks {
		if ip.Equal(myNet.IP) {
			return true, nil
		}
	}

	// Check for other IPs in range and broadcast addresses.
	// Do this in a second loop, as an IP will match in
	// most cases and network matching is more expensive.
	for _, myNet := range myNetworks {
		if myNet.Contains(ip) {
			return false, nil
		}
	}

	// Could not find IP anywhere. Refresh network to be sure.
	err = refreshMyNetworks()
	if err != nil {
		return false, err
	}
	myNetworks = *myNetworksAtomic.Load()

	// Check against assigned IPs again.
	for _, myNet := range myNetworks {
		if ip.Equal(myNet.IP) {
			return true, nil
		}
	}
	return false, nil
}

// GetLocalNetwork uses the given IP to search for a network configured on the
// device and returns it.
func GetLocalNetwork(ip net.IP) (myNet *net.IPNet, err error) {
	// Check if the network changed.
	if myNetworksChanged() {
		err := refreshMyNetworks()
		if err != nil {
			return nil, err
		}
	}
	myNetworks := *myNetworksAtomic.Load()

	// Check if the IP address is in my networks.
	for _, myNet := range myNetworks {
		if myNet.Contains(ip) {
			return myNet, nil
		}
	}

	return nil, nil
}
