package customlists

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/netutils"
)

var (
	countryCodesFilterList      map[string]struct{}
	ipAddressesFilterList       map[string]struct{}
	autonomousSystemsFilterList map[uint]struct{}
	domainsFilterList           map[string]struct{}
)

const (
	rationForInvalidLinesUntilWarning = 0.1
	parseStatusNotificationID         = "customlists:parse-status"
	parseWarningNotificationID        = "customlists:parse-warning"
	zeroIPNotificationID              = "customlists:too-many-zero-ips"
)

func initFilterLists() {
	countryCodesFilterList = make(map[string]struct{})
	ipAddressesFilterList = make(map[string]struct{})
	autonomousSystemsFilterList = make(map[uint]struct{})
	domainsFilterList = make(map[string]struct{})
}

// IsLoaded returns whether a custom filter list is loaded.
func IsLoaded() bool {
	filterListLock.RLock()
	defer filterListLock.RUnlock()

	switch {
	case len(domainsFilterList) > 0:
		return true
	case len(ipAddressesFilterList) > 0:
		return true
	case len(countryCodesFilterList) > 0:
		return true
	case len(autonomousSystemsFilterList) > 0:
		return true
	default:
		return false
	}
}

func parseFile(filePath string) error {
	// Reset all maps, previous (if any) settings will be lost.
	for key := range countryCodesFilterList {
		delete(countryCodesFilterList, key)
	}
	for key := range ipAddressesFilterList {
		delete(ipAddressesFilterList, key)
	}
	for key := range autonomousSystemsFilterList {
		delete(autonomousSystemsFilterList, key)
	}
	for key := range domainsFilterList {
		delete(domainsFilterList, key)
	}

	// Ignore empty file path.
	if filePath == "" {
		return nil
	}

	// Open the file if possible
	file, err := os.Open(filePath)
	if err != nil {
		log.Warningf("intel/customlists: failed to parse file %s", err)
		module.states.Add(mgr.State{
			ID:      parseWarningNotificationID,
			Name:    "Failed to open custom filter list",
			Message: err.Error(),
			Type:    mgr.StateTypeWarning,
		})
		return err
	} else {
		module.states.Remove(parseWarningNotificationID)
	}
	defer func() { _ = file.Close() }()

	var allLinesCount uint64
	var invalidLinesCount uint64

	// Read filter file line by line.
	scanner := bufio.NewScanner(file)
	// The scanner will error out if the line is greater than 64K, in this case it is enough.
	for scanner.Scan() {
		allLinesCount++
		// Parse and count invalid lines (comment, empty lines, zero IPs...)
		if !parseLine(scanner.Text()) {
			invalidLinesCount++
		}
	}

	// Check for scanner error.
	if err := scanner.Err(); err != nil {
		return err
	}

	invalidLinesRation := float32(invalidLinesCount) / float32(allLinesCount)

	if invalidLinesRation > rationForInvalidLinesUntilWarning {
		log.Warning("intel/customlists: Too many invalid lines")
		module.states.Add(mgr.State{
			ID:   zeroIPNotificationID,
			Name: "Custom filter list has many invalid lines",
			Message: fmt.Sprintf(`%d out of %d lines are invalid.
			 Check if you are using the correct file format and if the path to the custom filter list is correct.`, invalidLinesCount, allLinesCount),
			Type: mgr.StateTypeWarning,
		})
	} else {
		module.states.Remove(zeroIPNotificationID)
	}

	allEntriesCount := len(domainsFilterList) + len(ipAddressesFilterList) + len(autonomousSystemsFilterList) + len(countryCodesFilterList)
	log.Infof("intel/customlists: loaded %d entries from %s", allEntriesCount, filePath)

	notifications.NotifyInfo(parseStatusNotificationID,
		"Custom filter list loaded successfully.",
		fmt.Sprintf(`Custom filter list loaded from file %s:  
%d Domains  
%d IPs  
%d Autonomous Systems  
%d Countries`,
			filePath,
			len(domainsFilterList),
			len(ipAddressesFilterList),
			len(autonomousSystemsFilterList),
			len(countryCodesFilterList)))

	return nil
}

func parseLine(line string) (valid bool) {
	// Everything after the first field will be ignored.
	fields := strings.Fields(line)

	// Ignore empty lines.
	if len(fields) == 0 {
		return true // Not an entry, but a valid line.
	}

	field := fields[0]

	// Ignore comments
	if strings.HasPrefix(field, "#") {
		return true // Not an entry, but a valid line.
	}

	// Go through all possible field types.
	// Parsing is ordered by
	// 1. Parsing options (ie. the domain has most variation and goes last.)
	// 2. Speed

	// Check if it'a a country code.
	if isCountryCode(field) {
		countryCodesFilterList[field] = struct{}{}
		return true
	}

	// Check if it's a Autonomous system (example AS123).
	if isAutonomousSystem(field) {
		asNumber, err := strconv.ParseUint(field[2:], 10, 32)
		if err != nil {
			return false
		}
		autonomousSystemsFilterList[uint(asNumber)] = struct{}{}
		return true
	}

	// Try to parse IP address.
	ip := net.ParseIP(field)
	if ip != nil {
		// Check for zero ip.
		if net.IP.Equal(ip, net.IPv4zero) || net.IP.Equal(ip, net.IPv6zero) {
			return false
		}

		ipAddressesFilterList[ip.String()] = struct{}{}
		return true
	}

	// Check if it's a domain.
	domain := dns.Fqdn(field)
	if netutils.IsValidFqdn(domain) {
		domainsFilterList[domain] = struct{}{}
		return true
	}

	return false
}
