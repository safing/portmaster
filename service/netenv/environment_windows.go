package netenv

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils/osdetail"
)

// Gateways returns the currently active gateways.
func Gateways() []net.IP {
	defaultIf := getDefaultInterface()
	if defaultIf == nil {
		return nil
	}

	// Collect gateways.
	var gw []net.IP
	if defaultIf.IPv4DefaultGateway != nil {
		gw = append(gw, defaultIf.IPv4DefaultGateway)
	}
	if defaultIf.IPv6DefaultGateway != nil {
		gw = append(gw, defaultIf.IPv6DefaultGateway)
	}

	return gw
}

// Nameservers returns the currently active nameservers.
func Nameservers() []Nameserver {
	defaultIf := getDefaultInterface()
	if defaultIf == nil {
		return nil
	}

	// Compile search list.
	var search []string
	if defaultIf.DNSServerConfig != nil {
		if defaultIf.DNSServerConfig.Suffix != "" {
			search = append(search, defaultIf.DNSServerConfig.Suffix)
		}
		if len(defaultIf.DNSServerConfig.SuffixSearchList) > 0 {
			search = append(search, defaultIf.DNSServerConfig.SuffixSearchList...)
		}
	}

	// Compile nameservers.
	var ns []Nameserver
	for _, nsIP := range defaultIf.DNSServer {
		ns = append(ns, Nameserver{
			IP:     nsIP,
			Search: search,
		})
	}

	return ns
}

const (
	defaultInterfaceRecheck = 2 * time.Second
)

var (
	defaultInterface                   *defaultNetInterface
	defaultInterfaceLock               sync.Mutex
	defaultInterfaceNetworkChangedFlag = GetNetworkChangedFlag()
)

type defaultNetInterface struct {
	InterfaceIndex     string
	IPv6Address        net.IP
	IPv4Address        net.IP
	IPv6DefaultGateway net.IP
	IPv4DefaultGateway net.IP
	DNSServer          []net.IP
	DNSServerConfig    *dnsServerConfig
}

type dnsServerConfig struct {
	Suffix           string
	SuffixSearchList []string
}

func getDefaultInterface() *defaultNetInterface {
	defaultInterfaceLock.Lock()
	defer defaultInterfaceLock.Unlock()
	// Check if the network changed, if not, return cache.
	if !defaultInterfaceNetworkChangedFlag.IsSet() {
		return defaultInterface
	}
	defaultInterfaceNetworkChangedFlag.Refresh()

	// Get interface data from Windows.
	interfaceData, err := osdetail.RunPowershellCmd("Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Select-Object -First 1 | Get-NetIPConfiguration | Format-List")
	if err != nil {
		log.Warningf("netenv: failed to get interface data: %s", err)
		return nil
	}

	// TODO: It would be great to get this as json. Powershell can do this,
	// but it just spits out lots of weird data instead of the same strings
	// seen in the list.
	newIf := &defaultNetInterface{}

	// Scan data for needed fields.
	scanner := bufio.NewScanner(bytes.NewBuffer(interfaceData))
	scanner.Split(bufio.ScanLines)
	var segmentKey, segmentValue, previousKey string
	for scanner.Scan() {
		segments := strings.SplitN(scanner.Text(), " : ", 2)

		// Check what the line gives us.
		switch len(segments) {
		case 2:
			// This is a new key and value.
			segmentKey = strings.TrimSpace(segments[0])
			segmentValue = strings.TrimSpace(segments[1])
			previousKey = segmentKey
		case 1:
			// This is another value for the previous key.
			segmentKey = previousKey
			segmentValue = strings.TrimSpace(segments[0])
		default:
			continue
		}

		// Ignore empty lines.
		if segmentValue == "" {
			continue
		}

		// Parse and assign value to struct.
		switch segmentKey {
		case "InterfaceIndex":
			newIf.InterfaceIndex = segmentValue
		case "IPv6Address":
			newIf.IPv6Address = net.ParseIP(segmentValue)
		case "IPv4Address":
			newIf.IPv4Address = net.ParseIP(segmentValue)
		case "IPv6DefaultGateway":
			newIf.IPv6DefaultGateway = net.ParseIP(segmentValue)
		case "IPv4DefaultGateway":
			newIf.IPv4DefaultGateway = net.ParseIP(segmentValue)
		case "DNSServer":
			newIP := net.ParseIP(segmentValue)
			if newIP != nil {
				newIf.DNSServer = append(newIf.DNSServer, newIP)
			}
		}
	}

	// Get Search Scopes for this interface.
	if newIf.InterfaceIndex != "" {
		dnsConfigData, err := osdetail.RunPowershellCmd(fmt.Sprintf(
			"Get-DnsClient -InterfaceIndex %s | ConvertTo-Json -Depth 1",
			newIf.InterfaceIndex,
		))
		if err != nil {
			log.Warningf("netenv: failed to get dns server config data: %s", err)
		} else {
			// Parse data into struct.
			dnsConfig := &dnsServerConfig{}
			err := json.Unmarshal([]byte(dnsConfigData), dnsConfig)
			if err != nil {
				log.Warningf("netenv: failed to get dns server config data: %s", err)
			} else {
				newIf.DNSServerConfig = dnsConfig
			}
		}
	} else {
		log.Warning("netenv: could not get dns server config data, because default interface index is missing")
	}

	// Assign new value to cache and return.
	defaultInterface = newIf
	return defaultInterface
}
