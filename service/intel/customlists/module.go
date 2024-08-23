package customlists

import (
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/mgr"
)

type CustomList struct {
	mgr      *mgr.Manager
	instance instance

	updateFilterListWorkerMgr *mgr.WorkerMgr

	states *mgr.StateMgr
}

func (cl *CustomList) Manager() *mgr.Manager {
	return cl.mgr
}

func (cl *CustomList) States() *mgr.StateMgr {
	return cl.states
}

func (cl *CustomList) Start() error {
	return start()
}

func (cl *CustomList) Stop() error {
	return nil
}

// Helper variables for parsing the input file.
var (
	isCountryCode      = regexp.MustCompile("^[A-Z]{2}$").MatchString
	isAutonomousSystem = regexp.MustCompile(`^AS[0-9]+$`).MatchString
)

var (
	filterListFilePath         string
	filterListFileModifiedTime time.Time

	filterListLock sync.RWMutex

	// ErrNotConfigured is returned when updating the custom filter list, but it
	// is not configured.
	ErrNotConfigured = errors.New("custom filter list not configured")
)

func prep() error {
	initFilterLists()

	// Register the config in the ui.
	err := registerConfig()
	if err != nil {
		return err
	}

	// Register api endpoint for updating the filter list.
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:  "customlists/update",
		Write: api.PermitUser,
		ActionFunc: func(ar *api.Request) (msg string, err error) {
			errCheck := checkAndUpdateFilterList(nil)
			if errCheck != nil {
				return "", errCheck
			}
			return "Custom filter list loaded successfully.", nil
		},
		Name:        "Update custom filter list",
		Description: "Reload the filter list from the configured file.",
	}); err != nil {
		return err
	}

	return nil
}

func start() error {
	// Register to hook to update after config change.
	module.instance.Config().EventConfigChange.AddCallback(
		"update custom filter list",
		func(wc *mgr.WorkerCtx, _ struct{}) (bool, error) {
			err := checkAndUpdateFilterList(wc)
			if !errors.Is(err, ErrNotConfigured) {
				return false, err
			}
			return false, nil
		},
	)

	// Create parser task and enqueue for execution. "checkAndUpdateFilterList" will schedule the next execution.
	module.updateFilterListWorkerMgr.Delay(20 * time.Second).Repeat(1 * time.Minute)

	return nil
}

func checkAndUpdateFilterList(_ *mgr.WorkerCtx) error {
	filterListLock.Lock()
	defer filterListLock.Unlock()

	// Get path and return error if empty
	filePath := getFilePath()
	if filePath == "" {
		return ErrNotConfigured
	}

	// Try to get file info
	modifiedTime := time.Now()
	if fileInfo, err := os.Stat(filePath); err == nil {
		modifiedTime = fileInfo.ModTime()
	}

	// Check if file path has changed or if modified time has changed
	if filterListFilePath != filePath || !filterListFileModifiedTime.Equal(modifiedTime) {
		err := parseFile(filePath)
		if err != nil {
			return err
		}
		filterListFileModifiedTime = modifiedTime
		filterListFilePath = filePath
	}
	return nil
}

// LookupIP checks if the IP address is in a custom filter list.
func LookupIP(ip net.IP) bool {
	filterListLock.RLock()
	defer filterListLock.RUnlock()

	_, ok := ipAddressesFilterList[ip.String()]
	return ok
}

// LookupDomain checks if the Domain is in a custom filter list.
func LookupDomain(fullDomain string, filterSubdomains bool) (bool, string) {
	filterListLock.RLock()
	defer filterListLock.RUnlock()

	if filterSubdomains {
		// Check if domain is in the list and all its subdomains.
		listOfDomains := splitDomain(fullDomain)
		for _, domain := range listOfDomains {
			_, ok := domainsFilterList[domain]
			if ok {
				return true, domain
			}
		}
	} else {
		// Check only if the domain is in the list
		_, ok := domainsFilterList[fullDomain]
		return ok, fullDomain
	}
	return false, ""
}

// LookupASN checks if the Autonomous system number is in a custom filter list.
func LookupASN(number uint) bool {
	filterListLock.RLock()
	defer filterListLock.RUnlock()

	_, ok := autonomousSystemsFilterList[number]
	return ok
}

// LookupCountry checks if the country code is in a custom filter list.
func LookupCountry(countryCode string) bool {
	filterListLock.RLock()
	defer filterListLock.RUnlock()

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

var (
	module     *CustomList
	shimLoaded atomic.Bool
)

// New returns a new CustomList module.
func New(instance instance) (*CustomList, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("CustomList")
	module = &CustomList{
		mgr:      m,
		instance: instance,

		states: mgr.NewStateMgr(m),
		updateFilterListWorkerMgr: m.NewWorkerMgr(
			"update custom filter list",
			func(ctx *mgr.WorkerCtx) error {
				err := checkAndUpdateFilterList(ctx)
				if !errors.Is(err, ErrNotConfigured) {
					return err
				}
				return nil
			},
			nil,
		),
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	Config() *config.Config
}
