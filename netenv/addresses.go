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
	myIPs     []net.IP
	myIPsLock sync.Mutex
)

// IsMyIP returns whether the given IP is currently configured on the local host.
func IsMyIP(ip net.IP) (yes bool, err error) {
	if netutils.IPIsLocalhost(ip) {
		return true, nil
	}

	myIPsLock.Lock()
	defer myIPsLock.Unlock()

	// check
	for _, myIP := range myIPs {
		if ip.Equal(myIP) {
			return true, nil
		}
	}

	// refresh IPs
	myAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return false, fmt.Errorf("failed to refresh interface addresses: %s", err)
	}
	myIPs = make([]net.IP, 0, len(myAddrs))
	for _, addr := range myAddrs {
		netAddr, ok := addr.(*net.IPNet)
		if !ok {
			log.Warningf("netenv: interface address of unexpected type %T", addr)
			continue
		}

		myIPs = append(myIPs, netAddr.IP)
	}

	// check again
	for _, myIP := range myIPs {
		if ip.Equal(myIP) {
			return true, nil
		}
	}

	return false, nil
}
