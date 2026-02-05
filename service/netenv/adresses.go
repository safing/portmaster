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

type localInterfaceInfo struct {
	name         string
	hardwareAddr net.HardwareAddr
	ipv4         *net.IP // Only the first non-loopback IPv4 address
	ipv6         *net.IP // Only the first non-loopback IPv6 address
}

var (
	localInterfacesLock        sync.Mutex
	localInterfacesChangedFlag = GetNetworkChangedFlag()
	localInterfaces            []localInterfaceInfo
	interfacesByIdentifier     map[string]localInterfaceInfo
)

func updateLocalInterfaces() {
	localInterfaces = []localInterfaceInfo{} //net.Interface{}
	interfacesByIdentifier = make(map[string]localInterfaceInfo)

	// Mark as refreshed after this function ends.
	defer localInterfacesChangedFlag.Refresh()

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return
	}

	var (
		ifAddressIpV4, ifAddressIpV6 *net.IP
	)

	// Search for matching interface
	for _, iface := range interfaces {
		ifAddressIpV4 = nil
		ifAddressIpV6 = nil

		// Skip interfaces that are not UP (not active)
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// Skip loopback addresses
			if ipNet.IP.IsLoopback() {
				continue
			}

			if ip4 := ipNet.IP.To4(); ip4 != nil {
				ifAddressIpV4 = &ip4
			} else if ip6 := ipNet.IP.To16(); ip6 != nil {
				ifAddressIpV6 = &ip6
			}
			if ifAddressIpV4 != nil && ifAddressIpV6 != nil {
				break
			}
		}

		if ifAddressIpV4 != nil || ifAddressIpV6 != nil {
			localInterfaces = append(localInterfaces, localInterfaceInfo{
				name:         iface.Name,
				hardwareAddr: iface.HardwareAddr,
				ipv4:         ifAddressIpV4,
				ipv6:         ifAddressIpV6,
			})
		}
	}
}

// GetLocalInterfaceIPs returns the IPv4 and IPv6 addresses of a network interface, or nil if not found.
// The interfaceIdentifier can be an interface name (e.g., "eth0"), MAC address, or IP address.
// Only active (UP) interfaces are considered, and loopback addresses are excluded.
// If an interface has multiple addresses of the same type, only the first is returned.
// Results are cached for performance and refreshed when the network configuration changes.
func GetLocalInterfaceIPs(interfaceIdentifier string) (localIpv4 *net.IP, localIpv6 *net.IP) {
	localInterfacesLock.Lock()
	defer localInterfacesLock.Unlock()

	if interfaceIdentifier == "" {
		return nil, nil
	}

	if localInterfacesChangedFlag.IsSet() || localInterfaces == nil || interfacesByIdentifier == nil {
		// No cache available, update interfaces.
		updateLocalInterfaces()
	} else {
		// Check if we have a cached result for this identifier.
		if v, ok := interfacesByIdentifier[interfaceIdentifier]; ok {
			return v.ipv4, v.ipv6
		}
	}

	// Parse the filter as an IP in case it's an IP address (returns nil if not a valid IP)
	filterIP := net.ParseIP(interfaceIdentifier)

	// Search for matching interface
	for _, iface := range localInterfaces {
		matched := false

		// Priority 1: Check if filter matches interface name (most common case)
		if iface.name == interfaceIdentifier {
			matched = true
		}

		// Priority 2: Check if filter matches MAC address
		if !matched && iface.hardwareAddr != nil && iface.hardwareAddr.String() == interfaceIdentifier {
			matched = true
		}

		// Priority 3: Check if filter matches an IP address on this interface
		if !matched && filterIP != nil {
			if (iface.ipv4 != nil && iface.ipv4.Equal(filterIP)) || (iface.ipv6 != nil && iface.ipv6.Equal(filterIP)) {
				matched = true
			}
		}

		// If we found a match, return the addresses and cache the result for future lookups
		if matched {
			interfacesByIdentifier[interfaceIdentifier] = iface
			return iface.ipv4, iface.ipv6
		}
	}

	return nil, nil
}
