package customlists

import (
	"context"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/modules"
	"golang.org/x/net/publicsuffix"
)

var module *modules.Module

const (
	configModuleName  = "config"
	configChangeEvent = "config change"
)

// Helper variables for parsing the input file.
var (
	isCountryCode      = regexp.MustCompile("^[A-Z]{2}$").MatchString
	isAutonomousSystem = regexp.MustCompile(`^AS[0-9]+$`).MatchString
)

var (
	filterListFilePath         string
	filterListFileModifiedTime time.Time

	parseLock sync.RWMutex
)

func init() {
	module = modules.Register("customlists", prep, start, nil, "base")
}

func prep() error {
	// register the config in the ui
	err := registerConfig()
	if err != nil {
		return err
	}

	return nil
}

func start() error {
	// register to hook to update after config change.
	if err := module.RegisterEventHook(
		configModuleName,
		configChangeEvent,
		"update custom filter list",
		func(ctx context.Context, obj interface{}) error {
			_ = checkAndUpdateFilterList()
			return nil
		},
	); err != nil {
		return err
	}

	// register timer to run every periodically and check for file updates
	module.NewTask("intel/customlists file update check", func(context.Context, *modules.Task) error {
		_ = checkAndUpdateFilterList()
		return nil
	}).Repeat(10 * time.Minute)

	// parse the file at startup
	_ = parseFile(getFilePath())
	return nil
}

func checkAndUpdateFilterList() error {
	parseLock.Lock()
	defer parseLock.Unlock()

	// get path and try to get its info
	filePath := getFilePath()
	modifiedTime := time.Now()
	if fileInfo, err := os.Stat(filePath); err == nil {
		modifiedTime = fileInfo.ModTime()
	}

	// check if file path has changed or if modified time has changed
	if filterListFilePath != filePath || !filterListFileModifiedTime.Equal(modifiedTime) {
		err := parseFile(filePath)
		if err != nil {
			return nil
		}
		filterListFileModifiedTime = modifiedTime
		filterListFilePath = filePath
	}
	return nil
}

// LookupIP checks if the IP address is in a custom filter list.
func LookupIP(ip *net.IP) bool {
	_, ok := ipAddressesFilterList[ip.String()]
	return ok
}

// LookupDomain checks if the Domain is in a custom filter list.
func LookupDomain(fullDomain string, filterSubdomains bool) bool {
	if filterSubdomains {
		// check if domain is in the list and all its subdomains
		listOfDomains := splitDomain(fullDomain)
		for _, domain := range listOfDomains {
			_, ok := domainsFilterList[domain]
			if ok {
				return true
			}
		}
	} else {
		// check only if the domain is in the list
		_, ok := domainsFilterList[fullDomain]
		return ok
	}
	return false
}

// LookupASN checks if the Autonomous system number is in a custom filter list.
func LookupASN(number uint) bool {
	_, ok := autonomousSystemsFilterList[number]
	return ok
}

// LookupCountry checks if the country code is in a custom filter list.
func LookupCountry(countryCode string) bool {
	_, ok := countryCodesFilterList[countryCode]
	return ok
}

func splitDomain(domain string) []string {
	domain = strings.Trim(domain, ".")
	suffix, _ := publicsuffix.PublicSuffix(domain)
	if suffix == domain {
		return []string{domain}
	}

	domainWithoutSuffix := domain[:len(domain)-len(suffix)]
	domainWithoutSuffix = strings.Trim(domainWithoutSuffix, ".")

	splitted := strings.FieldsFunc(domainWithoutSuffix, func(r rune) bool {
		return r == '.'
	})

	domains := make([]string, 0, len(splitted))
	for idx := range splitted {

		d := strings.Join(splitted[idx:], ".") + "." + suffix
		if d[len(d)-1] != '.' {
			d += "."
		}
		domains = append(domains, d)
	}
	return domains
}
