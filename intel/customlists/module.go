package customlists

import (
	"bufio"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/network/netutils"
)

var module *modules.Module

// Helper variables for parsing the input file
var (
	countryCodes       = map[string]struct{}{"AF": {}, "AX": {}, "AL": {}, "DZ": {}, "AS": {}, "AD": {}, "AO": {}, "AI": {}, "AQ": {}, "AG": {}, "AR": {}, "AM": {}, "AW": {}, "AU": {}, "AT": {}, "AZ": {}, "BH": {}, "BS": {}, "BD": {}, "BB": {}, "BY": {}, "BE": {}, "BZ": {}, "BJ": {}, "BM": {}, "BT": {}, "BO": {}, "BQ": {}, "BA": {}, "BW": {}, "BV": {}, "BR": {}, "IO": {}, "BN": {}, "BG": {}, "BF": {}, "BI": {}, "KH": {}, "CM": {}, "CA": {}, "CV": {}, "KY": {}, "CF": {}, "TD": {}, "CL": {}, "CN": {}, "CX": {}, "CC": {}, "CO": {}, "KM": {}, "CG": {}, "CD": {}, "CK": {}, "CR": {}, "CI": {}, "HR": {}, "CU": {}, "CW": {}, "CY": {}, "CZ": {}, "DK": {}, "DJ": {}, "DM": {}, "DO": {}, "EC": {}, "EG": {}, "SV": {}, "GQ": {}, "ER": {}, "EE": {}, "ET": {}, "FK": {}, "FO": {}, "FJ": {}, "FI": {}, "FR": {}, "GF": {}, "PF": {}, "TF": {}, "GA": {}, "GM": {}, "GE": {}, "DE": {}, "GH": {}, "GI": {}, "GR": {}, "GL": {}, "GD": {}, "GP": {}, "GU": {}, "GT": {}, "GG": {}, "GN": {}, "GW": {}, "GY": {}, "HT": {}, "HM": {}, "VA": {}, "HN": {}, "HK": {}, "HU": {}, "IS": {}, "IN": {}, "ID": {}, "IR": {}, "IQ": {}, "IE": {}, "IM": {}, "IL": {}, "IT": {}, "JM": {}, "JP": {}, "JE": {}, "JO": {}, "KZ": {}, "KE": {}, "KI": {}, "KP": {}, "KR": {}, "KW": {}, "KG": {}, "LA": {}, "LV": {}, "LB": {}, "LS": {}, "LR": {}, "LY": {}, "LI": {}, "LT": {}, "LU": {}, "MO": {}, "MK": {}, "MG": {}, "MW": {}, "MY": {}, "MV": {}, "ML": {}, "MT": {}, "MH": {}, "MQ": {}, "MR": {}, "MU": {}, "YT": {}, "MX": {}, "FM": {}, "MD": {}, "MC": {}, "MN": {}, "ME": {}, "MS": {}, "MA": {}, "MZ": {}, "MM": {}, "NA": {}, "NR": {}, "NP": {}, "NL": {}, "NC": {}, "NZ": {}, "NI": {}, "NE": {}, "NG": {}, "NU": {}, "NF": {}, "MP": {}, "NO": {}, "OM": {}, "PK": {}, "PW": {}, "PS": {}, "PA": {}, "PG": {}, "PY": {}, "PE": {}, "PH": {}, "PN": {}, "PL": {}, "PT": {}, "PR": {}, "QA": {}, "RE": {}, "RO": {}, "RU": {}, "RW": {}, "BL": {}, "SH": {}, "KN": {}, "LC": {}, "MF": {}, "PM": {}, "VC": {}, "WS": {}, "SM": {}, "ST": {}, "SA": {}, "SN": {}, "RS": {}, "SC": {}, "SL": {}, "SG": {}, "SX": {}, "SK": {}, "SI": {}, "SB": {}, "SO": {}, "ZA": {}, "GS": {}, "SS": {}, "ES": {}, "LK": {}, "SD": {}, "SR": {}, "SJ": {}, "SZ": {}, "SE": {}, "CH": {}, "SY": {}, "TW": {}, "TJ": {}, "TZ": {}, "TH": {}, "TL": {}, "TG": {}, "TK": {}, "TO": {}, "TT": {}, "TN": {}, "TR": {}, "TM": {}, "TC": {}, "TV": {}, "UG": {}, "UA": {}, "AE": {}, "GB": {}, "US": {}, "UM": {}, "UY": {}, "UZ": {}, "VU": {}, "VE": {}, "VN": {}, "VG": {}, "VI": {}, "WF": {}, "EH": {}, "YE": {}, "ZM": {}, "ZW": {}}
	isAutonomousSystem = regexp.MustCompile(`^AS[0-9]+$`).MatchString
)

var (
	filteredCountryCodes      map[string]struct{}
	filteredIPAddresses       map[string]struct{}
	filteredAutonomousSystems map[string]struct{}
	filteredDomains           map[string]struct{}
)

func init() {
	module = modules.Register("customlists", prep, nil, nil, "base")
}

func prep() error {
	filteredCountryCodes = make(map[string]struct{})
	filteredIPAddresses = make(map[string]struct{})
	filteredAutonomousSystems = make(map[string]struct{})
	filteredDomains = make(map[string]struct{})

	file, err := os.Open("/home/vladimir/Dev/Safing/filterlists/custom.txt")
	if err != nil {
		return err
	}
	defer file.Close()

	// read filter file line by line
	scanner := bufio.NewScanner(file)
	// the scanner will error out if the line is greater than 64K, in this case it is enough
	for scanner.Scan() {
		parseFilterLine(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	log.Criticalf("filteredCountryCodes: %v", filteredCountryCodes)
	log.Criticalf("filteredIPAddresses: %v", filteredIPAddresses)
	log.Criticalf("filteredAutonomousSystems: %v", filteredAutonomousSystems)
	log.Criticalf("filteredDomains: %v", filteredDomains)

	return nil
}

func parseFilterLine(line string) {
	// ignore empty lines and comment lines
	if len(line) == 0 || line[0] == '#' {
		return
	}

	fields := strings.Fields(line)

	// everything after the first field will be ignored
	firstField := fields[0]

	// check if it'a a country code
	if _, ok := countryCodes[firstField]; ok {
		filteredCountryCodes[firstField] = struct{}{}
	}

	// try to parse IP address
	ip := net.ParseIP(firstField)
	if ip != nil {
		filteredIPAddresses[ip.String()] = struct{}{}
	}

	// check if it's a Autonomous system (example AS123)
	if isAutonomousSystem(firstField) {
		filteredAutonomousSystems[firstField] = struct{}{}
	}

	// check if it's a domain
	potentialDomain := dns.Fqdn(firstField)
	if netutils.IsValidFqdn(potentialDomain) {
		filteredDomains[potentialDomain] = struct{}{}
	}
}

// LookupIPv4 checks if the IP is in a custom filter list
func LookupIPv4(ip *net.IP) bool {
	log.Debugf("Checking ip %s", ip.String())
	_, ok := filteredIPAddresses[ip.String()]
	return ok
}

// LookupDomain checks if the Domain is in a custom filter list
func LookupDomain(domain string) bool {
	log.Debugf("Checking domain %s", domain)
	_, ok := filteredDomains[domain]
	return ok
}
