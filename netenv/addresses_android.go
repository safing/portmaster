package netenv

import (
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster-android/go/app_interface"
)

// GetAssignedAddresses returns the assigned IPv4 and IPv6 addresses of the host.
func GetAssignedAddresses() (ipv4 []net.IP, ipv6 []net.IP, err error) {
	addrs, err := app_interface.GetNetworkAddresses()
	if err != nil {
		return nil, nil, err
	}
	for _, addr := range addrs {
		netAddr := addr.ToIPNet()
		if netAddr != nil {
			if ip4 := netAddr.IP.To4(); ip4 != nil {
				ipv4 = append(ipv4, ip4)
			} else {
				ipv6 = append(ipv6, netAddr.IP)
			}
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
	addresses, err := app_interface.GetNetworkAddresses()
	if err != nil {
		// In some cases the system blocks on this call, which piles up to
		// literally over thousand goroutines wanting to try this again.
		myNetworksRefreshError = err
		return fmt.Errorf("failed to refresh interface addresses: %w", err)
	}
	myNetworks = make([]*net.IPNet, 0, len(addresses))
	for _, ifNet := range addresses {
		ipNet := ifNet.ToIPNet()
		myNetworks = append(myNetworks, ipNet)
	}

	// Reset changed flag.
	myNetworksNetworkChangedFlag.Refresh()

	return nil
}
