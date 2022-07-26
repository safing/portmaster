package customlists

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/netutils"
)

var (
	countryCodesFilterList      map[string]struct{}
	ipAddressesFilterList       map[string]struct{}
	autonomousSystemsFilterList map[uint]struct{}
	domainsFilterList           map[string]struct{}
)

func parseFile(filePath string) error {
	// open the file if possible
	file, err := os.Open(filePath)
	if err != nil {
		log.Warningf("Custom filter: failed to parse file: \"%s\"", filePath)
		return err
	}
	defer file.Close()

	// initialize maps to hold data from the file
	countryCodesFilterList = make(map[string]struct{})
	ipAddressesFilterList = make(map[string]struct{})
	autonomousSystemsFilterList = make(map[uint]struct{})
	domainsFilterList = make(map[string]struct{})

	// read filter file line by line
	scanner := bufio.NewScanner(file)
	// the scanner will error out if the line is greater than 64K, in this case it is enough
	for scanner.Scan() {
		parseLine(scanner.Text())
	}

	// check for scanner error
	if err := scanner.Err(); err != nil {
		return err
	}

	log.Infof("Custom filter: list loaded successful: %s", filePath)

	return nil
}

func parseLine(line string) {
	// ignore empty lines and comment lines
	if len(line) == 0 || line[0] == '#' {
		return
	}

	// everything after the first field will be ignored
	field := strings.Fields(line)[0]

	// check if it'a a country code
	if isCountryCode(field) {
		countryCodesFilterList[field] = struct{}{}
	}

	// try to parse IP address
	ip := net.ParseIP(field)
	if ip != nil {
		ipAddressesFilterList[ip.String()] = struct{}{}
	}

	// check if it's a Autonomous system (example AS123)
	if isAutonomousSystem(field) {
		asNumber, err := strconv.ParseUint(field[2:], 10, 32)
		if err != nil {
			return
		}
		autonomousSystemsFilterList[uint(asNumber)] = struct{}{}
	}

	// check if it's a domain
	domain := dns.Fqdn(field)
	if netutils.IsValidFqdn(domain) {
		domainsFilterList[domain] = struct{}{}
	}
}
