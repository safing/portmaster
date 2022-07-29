package customlists

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
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
		log.Warningf("intel/customlists: failed to parse file %q ", err)
		module.Warning(parseStatusNotificationID, "Failed to open custom filter list", err.Error())
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

	var invalidLinesRation float32 = float32(invalidLinesCount) / float32(allLinesCount)

	if invalidLinesRation > rationForInvalidLinesUntilWarning {
		log.Warning("intel/customlists: Too many invalid lines")
		module.Warning(zeroIPNotificationID, "Check your custom filter list, there is too many invalid lines",
			fmt.Sprintf(`There are %d from total %d lines that we flagged as invalid.
			 Check if you are using the correct file format or if the path to the custom filter list is correct.`, invalidLinesCount, allLinesCount))
	} else {
		module.Resolve(zeroIPNotificationID)
	}

	log.Infof("intel/customlists: list loaded successful: %s", filePath)

	notifications.NotifyInfo(parseStatusNotificationID,
		"Custom filter list loaded successfully.",
		fmt.Sprintf(`Custom filter list loaded successfully from file %s  
%d domains  
%d IPs  
%d autonomous systems  
%d countries`,
			filePath,
			len(domainsFilterList),
			len(ipAddressesFilterList),
			len(autonomousSystemsFilterList),
			len(domainsFilterList)))

	module.Resolve(parseStatusNotificationID)

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
		if bytes.Compare(ip, net.IPv4zero) == 0 || bytes.Compare(ip, net.IPv6zero) == 0 {
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
