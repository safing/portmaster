//go:build !android

package netenv

import (
	"fmt"
	"net"
	"time"

	"github.com/safing/portbase/log"
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
	interfaceNetworks, err := net.InterfaceAddrs()
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
