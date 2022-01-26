package netenv

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/netutils"
)

// GetAssignedAddresses returns the assigned IPv4 and IPv6 addresses of the host.
func GetAssignedAddresses() (ipv4 []net.IP, ipv6 []net.IP, err error) {
	addrs, err := net.InterfaceAddrs()
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
	myNetworks                    []*net.IPNet
	myNetworksLock                sync.Mutex
	myNetworksNetworkChangedFlag  = GetNetworkChangedFlag()
	myNetworksRefreshError        error
	myNetworksRefreshFailingUntil time.Time
)

// IsMyIP returns whether the given unicast IP is currently configured on the local host.
// Broadcast or multicast addresses will never match, even if valid in in use.
func IsMyIP(ip net.IP) (yes bool, err error) {
	// Check for IPs that don't need extra checks.
	switch netutils.GetIPScope(ip) {
	case netutils.HostLocal:
		return true, nil
	case netutils.LocalMulticast, netutils.GlobalMulticast:
		return false, nil
	}

	myNetworksLock.Lock()
	defer myNetworksLock.Unlock()

	// Check if current data matches IP.
	// Matching on somewhat older data is not a problem, as these IPs would not
	// just randomly pop up somewhere else that fast.
	mine, myNet := checkIfMyIP(ip)
	switch {
	case mine:
		// IP matched.
		return true, nil
	case myNetworksNetworkChangedFlag.IsSet():
		// The network changed, so we need to refresh the data.
	case myNet:
		// IP is one of the networks and nothing changed, so this is not our IP.
		return false, nil
	}

	// Check if there was a recent error on the previous refresh.
	if myNetworksRefreshError != nil && time.Now().Before(myNetworksRefreshFailingUntil) {
		return false, fmt.Errorf("failed to previously refresh interface addresses: %s", myNetworksRefreshError)
	}

	// Refresh assigned networks.
	interfaceNetworks, err := net.InterfaceAddrs()
	if err != nil {
		// Save error for one second.
		// In some cases the system blocks on this call, which piles up to
		// literally over thousand goroutines wanting to try this again.
		myNetworksRefreshError = err
		myNetworksRefreshFailingUntil = time.Now().Add(1 * time.Second)
		return false, fmt.Errorf("failed to refresh interface addresses: %s", err)
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

	// Reset error.
	myNetworksRefreshError = nil

	// Reset changed flag.
	myNetworksNetworkChangedFlag.Refresh()

	// Check for match again.
	if mine, matched := checkIfMyIP(ip); matched {
		return mine, nil
	}
	return false, nil
}

func checkIfMyIP(ip net.IP) (mine bool, myNet bool) {
	// Check against assigned IPs.
	for _, myNet := range myNetworks {
		if ip.Equal(myNet.IP) {
			return true, true
		}
	}

	// Check for other IPs in range and broadcast addresses.
	// Do this in a second loop, as an IP will match in
	// most cases and network matching is more expensive.
	for _, myNet := range myNetworks {
		if myNet.Contains(ip) {
			return false, true
		}
	}

	return false, false
}
