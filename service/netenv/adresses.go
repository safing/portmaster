package netenv

import (
	"fmt"
	"net"
	"sync"
	"time"

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
	myNetworks                   []*net.IPNet
	myNetworksLock               sync.Mutex
	myNetworksNetworkChangedFlag = GetNetworkChangedFlag()
	myNetworksRefreshError       error //nolint:errname // Not what the linter thinks this is for.
	myNetworksDontRefreshUntil   time.Time
)

// refreshMyNetworks refreshes the networks held in refreshMyNetworks.
// The caller must hold myNetworksLock.
func refreshMyNetworks() error {
	// Check if we already refreshed recently.
	if time.Now().Before(myNetworksDontRefreshUntil) {
		// Return previous error, if available.
		if myNetworksRefreshError != nil {
			return fmt.Errorf("failed to previously refresh interface addresses: %w", myNetworksRefreshError)
		}
		return nil
	}
	myNetworksRefreshError = nil
	myNetworksDontRefreshUntil = time.Now().Add(1 * time.Second)

	// Refresh assigned networks.
	interfaceNetworks, err := osGetInterfaceAddrs()
	if err != nil {
		// In some cases the system blocks on this call, which piles up to
		// literally over thousand goroutines wanting to try this again.
		myNetworksRefreshError = err
		return fmt.Errorf("failed to refresh interface addresses: %w", err)
	}
	myNetworks = make([]*net.IPNet, 0, len(interfaceNetworks))
	for _, ifNet := range interfaceNetworks {
		ipNet, ok := ifNet.(*net.IPNet)
		if !ok {
			log.Warningf("netenv: interface network of unexpected type %T", ifNet)
			continue
		}

		myNetworks = append(myNetworks, ipNet)
	}

	// Reset changed flag.
	myNetworksNetworkChangedFlag.Refresh()

	return nil
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

	myNetworksLock.Lock()
	defer myNetworksLock.Unlock()

	// Check if the network changed.
	if myNetworksNetworkChangedFlag.IsSet() {
		err := refreshMyNetworks()
		if err != nil {
			return false, err
		}
	}

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
	myNetworksLock.Lock()
	defer myNetworksLock.Unlock()

	// Check if the network changed.
	if myNetworksNetworkChangedFlag.IsSet() {
		err := refreshMyNetworks()
		if err != nil {
			return nil, err
		}
	}

	// Check if the IP address is in my networks.
	for _, myNet := range myNetworks {
		if myNet.Contains(ip) {
			return myNet, nil
		}
	}

	return nil, nil
}
