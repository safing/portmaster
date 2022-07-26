package customlists

import (
	"context"
	"net"
	"os"
	"regexp"
	"time"

	"github.com/safing/portbase/modules"
)

var module *modules.Module

const configChangeEvent = "config change"

// Helper variables for parsing the input file
var (
	isCountryCode      = regexp.MustCompile("^[A-Z]{2}$").MatchString
	isAutonomousSystem = regexp.MustCompile(`^AS[0-9]+$`).MatchString
)

var (
	filterListFilePath         string
	filterListFileModifiedTime time.Time
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

	// register to hook to update after config change.
	if err := module.RegisterEventHook(
		module.Name,
		configChangeEvent,
		"update custom filter list",
		func(ctx context.Context, obj interface{}) error {
			_ = checkAndUpdateFilterList()
			return nil
		},
	); err != nil {
		return err
	}

	return nil
}

func start() error {
	// register timer to run every periodically and check for file updates
	module.NewTask("Custom filter list file update check", func(context.Context, *modules.Task) error {
		_ = checkAndUpdateFilterList()
		return nil
	}).Repeat(10 * time.Minute)

	// parse the file for the first time at start
	_ = parseFile(getFilePath())
	return nil
}

func checkAndUpdateFilterList() error {
	// get path and try to get its info
	filePath := getFilePath()
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil
	}
	modifiedTime := fileInfo.ModTime()

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

// LookupIP checks if the IP address is in a custom filter list
func LookupIP(ip *net.IP) bool {
	_, ok := ipAddressesFilterList[ip.String()]
	return ok
}

// LookupDomain checks if the Domain is in a custom filter list
func LookupDomain(domain string) bool {
	_, ok := domainsFilterList[domain]
	return ok
}

// LookupASN checks if the Autonomous system number is in a custom filter list
func LookupASN(number uint) bool {
	_, ok := autonomousSystemsFilterList[number]
	return ok
}

// LookupCountry checks if the country code is in a custom filter list
func LookupCountry(countryCode string) bool {
	_, ok := countryCodesFilterList[countryCode]
	return ok
}
