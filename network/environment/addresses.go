package environment

import (
	"net"
	"strings"

	"github.com/Safing/portmaster/network/netutils"
)

func GetAssignedAddresses() (ipv4 []net.IP, ipv6 []net.IP, err error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, nil, err
	}
	for _, addr := range addrs {
		ip := net.ParseIP(strings.Split(addr.String(), "/")[0])
		if ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				ipv4 = append(ipv4, ip4)
			} else {
				ipv6 = append(ipv6, ip)
			}
		}
	}
	return
}

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
