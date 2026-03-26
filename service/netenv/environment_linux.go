package netenv

import (
	"bufio"
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/netutils"
)

var (
	gateways                   = make([]net.IP, 0)
	gatewaysInfo               = make([]GatewayInfo, 0) // same as gateways but with additional info
	gatewaysLock               sync.Mutex
	gatewaysNetworkChangedFlag = GetNetworkChangedFlag()

	nameservers                   = make([]Nameserver, 0)
	nameserversLock               sync.Mutex
	nameserversNetworkChangedFlag = GetNetworkChangedFlag()
)

type GatewayInfo struct {
	IP        net.IP
	Interface string
	Mask      net.IPMask
}

// Gateways returns the currently active gateways.
func Gateways() []net.IP {
	gatewaysLock.Lock()
	defer gatewaysLock.Unlock()
	refreshGatewaysCache()

	return gateways
}

// GatewaysInfo returns the currently active gateways with interface metadata.
func GatewaysInfo() []GatewayInfo {
	gatewaysLock.Lock()
	defer gatewaysLock.Unlock()

	refreshGatewaysCache()

	return gatewaysInfo
}

func refreshGatewaysCache() {
	// Check if the network changed, if not, keep cache.
	if !gatewaysNetworkChangedFlag.IsSet() {
		return
	}
	gatewaysNetworkChangedFlag.Refresh()

	gateways = make([]net.IP, 0)
	gatewaysInfo = make([]GatewayInfo, 0)
	var decoded []byte

	// open file
	route, err := os.Open("/proc/net/route")
	if err != nil {
		log.Warningf("environment: could not read /proc/net/route: %s", err)
		return
	}
	defer func() {
		_ = route.Close()
	}()

	// file scanner
	scanner := bufio.NewScanner(route)
	scanner.Split(bufio.ScanLines)

	// parse
	for scanner.Scan() {
		line := strings.Fields(scanner.Text())
		if len(line) < 4 {
			continue
		}
		iface := line[0]
		if line[1] == "00000000" {
			decoded, err = hex.DecodeString(line[2])
			if err != nil {
				log.Warningf("environment: could not parse gateway %s from /proc/net/route: %s", line[2], err)
				continue
			}
			if len(decoded) != 4 {
				log.Warningf("environment: decoded gateway %s from /proc/net/route has wrong length", decoded)
				continue
			}
			gate := net.IPv4(decoded[3], decoded[2], decoded[1], decoded[0])
			mask := net.IPv4Mask(0, 0, 0, 0)
			if len(line) > 7 {
				decodedMask, decodeMaskErr := hex.DecodeString(line[7])
				if decodeMaskErr != nil {
					log.Warningf("environment: could not parse netmask %s from /proc/net/route: %s", line[7], decodeMaskErr)
				} else if len(decodedMask) == 4 {
					mask = net.IPv4Mask(decodedMask[3], decodedMask[2], decodedMask[1], decodedMask[0])
				}
			}
			gateways = append(gateways, gate)
			gatewaysInfo = append(gatewaysInfo, GatewayInfo{IP: gate, Interface: iface, Mask: mask})
		}
	}

	// open file
	v6route, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		log.Warningf("environment: could not read /proc/net/ipv6_route: %s", err)
		return
	}
	defer func() {
		_ = v6route.Close()
	}()

	// file scanner
	scanner = bufio.NewScanner(v6route)
	scanner.Split(bufio.ScanLines)

	// parse
	for scanner.Scan() {
		line := strings.Fields(scanner.Text())
		if len(line) < 6 {
			continue
		}
		iface := line[len(line)-1]
		if line[0] == "00000000000000000000000000000000" && line[4] != "00000000000000000000000000000000" {
			mask := net.CIDRMask(0, 128)
			prefixLength, parsePrefixLengthErr := strconv.ParseInt(line[1], 16, 32)
			if parsePrefixLengthErr == nil {
				mask = net.CIDRMask(int(prefixLength), 128)
			}
			decoded, err := hex.DecodeString(line[4])
			if err != nil {
				log.Warningf("environment: could not parse gateway %s from /proc/net/ipv6_route: %s", line[2], err)
				continue
			}
			if len(decoded) != 16 {
				log.Warningf("environment: decoded gateway %s from /proc/net/ipv6_route has wrong length", decoded)
				continue
			}
			gate := net.IP(decoded)
			gateways = append(gateways, gate)
			gatewaysInfo = append(gatewaysInfo, GatewayInfo{IP: gate, Interface: iface, Mask: mask})
		}
	}
}

// Nameservers returns the currently active nameservers.
func Nameservers() []Nameserver {
	nameserversLock.Lock()
	defer nameserversLock.Unlock()
	// Check if the network changed, if not, return cache.
	if !nameserversNetworkChangedFlag.IsSet() {
		return nameservers
	}
	nameserversNetworkChangedFlag.Refresh()

	// logic
	// TODO: try:
	// 1. NetworkManager DBUS
	// 2. /etc/resolv.conf
	// 2.1. if /etc/resolv.conf has localhost nameserver, check for dnsmasq config (are there others?)
	nameservers = make([]Nameserver, 0)

	// get nameservers from DBUS
	dbusNameservers, err := getNameserversFromDbus()
	if err != nil {
		log.Warningf("environment: could not get nameservers from dbus: %s", err)
	} else {
		nameservers = addNameservers(nameservers, dbusNameservers)
	}

	// get nameservers from /etc/resolv.conf
	resolvconfNameservers, err := getNameserversFromResolvconf()
	if err != nil {
		log.Warningf("environment: could not get nameservers from resolvconf: %s", err)
	} else {
		nameservers = addNameservers(nameservers, resolvconfNameservers)
	}

	return nameservers
}

func getNameserversFromResolvconf() ([]Nameserver, error) {
	// open file
	resolvconf, err := os.Open("/etc/resolv.conf")
	if err != nil {
		log.Warningf("environment: could not read /etc/resolv.conf: %s", err)
		return nil, err
	}
	defer func() {
		_ = resolvconf.Close()
	}()

	// file scanner
	scanner := bufio.NewScanner(resolvconf)
	scanner.Split(bufio.ScanLines)

	var searchDomains []string
	var servers []net.IP

	// parse
	for scanner.Scan() {
		line := strings.SplitN(scanner.Text(), " ", 3)
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case "search":
			if netutils.IsValidFqdn(dns.Fqdn(line[1])) {
				searchDomains = append(searchDomains, line[1])
			}
		case "nameserver":
			ip := net.ParseIP(line[1])
			if ip != nil {
				servers = append(servers, ip)
			}
		}
	}

	// build array
	nameservers := make([]Nameserver, 0, len(servers))
	for _, server := range servers {
		nameservers = append(nameservers, Nameserver{
			IP:     server,
			Search: searchDomains,
		})
	}
	return nameservers, nil
}

func addNameservers(nameservers, newNameservers []Nameserver) []Nameserver {
	for _, newNameserver := range newNameservers {
		found := false
		for _, nameserver := range nameservers {
			if nameserver.IP.Equal(newNameserver.IP) {
				found = true
				break
			}
		}
		if !found {
			nameservers = append(nameservers, newNameserver)
		}
	}
	return nameservers
}
