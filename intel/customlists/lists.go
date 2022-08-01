package customlists

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log" //nolint  // weird error "Expected '\n', Found '\t'"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/network/netutils"
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

func parseFile(filePath string) error {
	// reset all maps, previous (if any) settings will be lost.
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

	// ignore empty file path.
	if filePath == "" {
		return nil
	}

	// open the file if possible
	file, err := os.Open(filePath)
	if err != nil {
		log.Warningf("intel/customlists: failed to parse file %s", err)
		module.Warning(parseWarningNotificationID, "Failed to open custom filter list", err.Error())
		return err
	}
	defer func() { _ = file.Close() }()

	var allLinesCount uint64
	var invalidLinesCount uint64

	// read filter file line by line.
	scanner := bufio.NewScanner(file)
	// the scanner will error out if the line is greater than 64K, in this case it is enough.
	for scanner.Scan() {
		allLinesCount++
		// parse and count invalid lines (comment, empty lines, zero IPs...)
		if !parseLine(scanner.Text()) {
			invalidLinesCount++
		}
	}

	// check for scanner error.
	if err := scanner.Err(); err != nil {
		return err
	}

	invalidLinesRation := float32(invalidLinesCount) / float32(allLinesCount)

	if invalidLinesRation > rationForInvalidLinesUntilWarning {
		log.Warning("intel/customlists: Too many invalid lines")
		module.Warning(zeroIPNotificationID, "Custom filter list has many invalid entries",
			fmt.Sprintf(`%d out of %d entires are invalid.
			 Check if you are using the correct file format and if the path to the custom filter list is correct.`, invalidLinesCount, allLinesCount))
	} else {
		module.Resolve(zeroIPNotificationID)
	}

	allEntriesCount := len(domainsFilterList) + len(ipAddressesFilterList) + len(autonomousSystemsFilterList) + len(countryCodesFilterList)
	log.Infof("intel/customlists: loaded %d entries from %s", allEntriesCount, filePath)

	notifications.NotifyInfo(parseStatusNotificationID,
		"Custom filter list loaded successfully.",
		fmt.Sprintf(`Custom filter list loaded successfully from file %s - loaded:  
%d Domains  
%d IPs  
%d Autonomous Systems  
%d Countries`,
			filePath,
			len(domainsFilterList),
			len(ipAddressesFilterList),
			len(autonomousSystemsFilterList),
			len(countryCodesFilterList)))

	module.Resolve(parseWarningNotificationID)

	return nil
}

func parseLine(line string) bool {
	// everything after the first field will be ignored.
	fields := strings.Fields(line)

	// ignore empty lines.
	if len(fields) == 0 {
		return false
	}

	field := fields[0]

	// ignore comments
	if field[0] == '#' {
		return false
	}

	// check if it'a a country code.
	if isCountryCode(field) {
		countryCodesFilterList[field] = struct{}{}
		return true
	}

	// try to parse IP address.
	ip := net.ParseIP(field)
	if ip != nil {
		// check for zero ip.
		if net.IP.Equal(ip, net.IPv4zero) || net.IP.Equal(ip, net.IPv6zero) {
			return false
		}

		ipAddressesFilterList[ip.String()] = struct{}{}
		return true
	}

	// check if it's a Autonomous system (example AS123).
	if isAutonomousSystem(field) {
		asNumber, err := strconv.ParseUint(field[2:], 10, 32)
		if err != nil {
			return false
		}
		autonomousSystemsFilterList[uint(asNumber)] = struct{}{}
		return true
	}

	// check if it's a domain.
	domain := dns.Fqdn(field)
	if netutils.IsValidFqdn(domain) {
		domainsFilterList[domain] = struct{}{}
		return true
	}

	return false
}
