package netenv

import (
	"fmt"
	"net"
	"sync"

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
		if netutils.IPIsGlobal(ip4) {
			ipv4 = append(ipv4, ip4)
		}
	}
	for _, ip6 := range allv6 {
		if netutils.IPIsGlobal(ip6) {
			ipv6 = append(ipv6, ip6)
		}
	}
	return
}

var (
	myNetworks     []*net.IPNet
	myNetworksLock sync.Mutex
)

// IsMyIP returns whether the given unicast IP is currently configured on the local host.
// Broadcast or multicast addresses will never match, even if valid in in use.
func IsMyIP(ip net.IP) (yes bool, err error) {
	// Check for IPs that don't need extra checks.
	switch netutils.ClassifyIP(ip) {
	case netutils.HostLocal:
		return true, nil
	case netutils.LocalMulticast, netutils.GlobalMulticast:
		return false, nil
	}

	myNetworksLock.Lock()
	defer myNetworksLock.Unlock()

	// Check for match.
	if mine, matched := checkIfMyIP(ip); matched {
		return mine, nil
	}

	// Refresh assigned networks.
	interfaceNetworks, err := net.InterfaceAddrs()
	if err != nil {
		return false, fmt.Errorf("failed to refresh interface addresses: %s", err)
	}
	myNetworks = make([]*net.IPNet, 0, len(interfaceNetworks))
	for _, ifNet := range interfaceNetworks {
		net, ok := ifNet.(*net.IPNet)
		if !ok {
			log.Warningf("netenv: interface network of unexpected type %T", ifNet)
			continue
		}

		myNetworks = append(myNetworks, net)
	}

	// Check for match again.
	if mine, matched := checkIfMyIP(ip); matched {
		return mine, nil
	}
	return false, nil
}

func checkIfMyIP(ip net.IP) (mine bool, matched bool) {
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
