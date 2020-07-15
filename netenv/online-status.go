package netenv

import (
	"context"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/netutils"

	"github.com/tevino/abool"
)

// OnlineStatus represent a state of connectivity to the Internet.
type OnlineStatus uint8

// Online Status Values
const (
	StatusUnknown    OnlineStatus = 0
	StatusOffline    OnlineStatus = 1
	StatusLimited    OnlineStatus = 2 // local network only
	StatusPortal     OnlineStatus = 3 // there seems to be an internet connection, but we are being intercepted, possibly by a captive portal
	StatusSemiOnline OnlineStatus = 4 // we seem to online, but without full connectivity
	StatusOnline     OnlineStatus = 5
)

// Online Status and Resolver
const (
	HTTPTestURL         = "http://detectportal.firefox.com/success.txt"
	HTTPExpectedContent = "success"
	HTTPSTestURL        = "https://one.one.one.one/"

	ResolverTestFqdn             = "one.one.one.one."
	ResolverTestRRType           = dns.TypeA
	ResolverTestExpectedResponse = "1.1.1.1"

	SpecialCaptivePortalDomain = "captiveportal.local."
)

var (
	parsedHTTPTestURL  *url.URL
	parsedHTTPSTestURL *url.URL
)

func init() {
	var err error

	parsedHTTPTestURL, err = url.Parse(HTTPTestURL)
	if err != nil {
		panic(err)
	}

	parsedHTTPSTestURL, err = url.Parse(HTTPSTestURL)
	if err != nil {
		panic(err)
	}
}

// IsConnectivityDomain checks whether the given domain (fqdn) is used for any connectivity related network connections and should always be resolved using the network assigned DNS server.
func IsConnectivityDomain(domain string) bool {
	switch domain {
	case "detectportal.firefox.com.",
		"one.one.one.one.",
		GetCaptivePortal().Domain:
		return true
	}

	return false
}

// GetResolverTestingRequestData returns request information that should be used to test DNS resolvers for availability and basic correct behaviour.
func GetResolverTestingRequestData() (fqdn string, rrType uint16, expectedResponse string) {
	return ResolverTestFqdn, ResolverTestRRType, ResolverTestExpectedResponse
}

func (os OnlineStatus) String() string {
	switch os {
	default:
		return "Unknown"
	case StatusOffline:
		return "Offline"
	case StatusLimited:
		return "Limited"
	case StatusPortal:
		return "Portal"
	case StatusSemiOnline:
		return "SemiOnline"
	case StatusOnline:
		return "Online"
	}
}

var (
	onlineStatus           *int32
	onlineStatusQuickCheck = abool.NewBool(false)

	onlineStatusInvestigationTrigger    = make(chan struct{}, 1)
	onlineStatusInvestigationInProgress = abool.NewBool(false)
	onlineStatusInvestigationWg         sync.WaitGroup

	captivePortal     = &CaptivePortal{}
	captivePortalLock sync.Mutex
)

// CaptivePortal holds information about a detected captive portal.
type CaptivePortal struct {
	URL    string
	Domain string
	IP     net.IP
}

func init() {
	var onlineStatusValue int32
	onlineStatus = &onlineStatusValue
}

// Online returns true if online status is either SemiOnline or Online.
func Online() bool {
	return onlineStatusQuickCheck.IsSet()
}

// GetOnlineStatus returns the current online stats.
func GetOnlineStatus() OnlineStatus {
	return OnlineStatus(atomic.LoadInt32(onlineStatus))
}

// CheckAndGetOnlineStatus triggers a new online status check and returns the result
func CheckAndGetOnlineStatus() OnlineStatus {
	// trigger new investigation
	triggerOnlineStatusInvestigation()
	// wait for completion
	onlineStatusInvestigationWg.Wait()
	// return current status
	return GetOnlineStatus()
}

func updateOnlineStatus(status OnlineStatus, portalURL, comment string) {
	changed := false

	// status
	currentStatus := atomic.LoadInt32(onlineStatus)
	if status != OnlineStatus(currentStatus) && atomic.CompareAndSwapInt32(onlineStatus, currentStatus, int32(status)) {
		// status changed!
		onlineStatusQuickCheck.SetTo(
			status == StatusOnline || status == StatusSemiOnline,
		)
		changed = true
	}

	// captive portal
	// delete if offline, update only if there is a new value
	if status == StatusOffline || portalURL != "" {
		setCaptivePortal(portalURL)
	}

	// trigger event
	if changed {
		module.TriggerEvent(OnlineStatusChangedEvent, nil)
		if status == StatusPortal {
			log.Infof(`network: setting online status to %s at "%s" (%s)`, status, portalURL, comment)
		} else {
			log.Infof("network: setting online status to %s (%s)", status, comment)
		}
		triggerNetworkChangeCheck()
	}
}

func setCaptivePortal(portalURL string) {
	captivePortalLock.Lock()
	defer captivePortalLock.Unlock()

	// delete
	if portalURL == "" {
		captivePortal = &CaptivePortal{}
		return
	}

	// set
	captivePortal = &CaptivePortal{
		URL: portalURL,
	}
	parsedURL, err := url.Parse(portalURL)
	switch {
	case err != nil:
		log.Debugf(`netenv: failed to parse captive portal URL "%s": %s`, portalURL, err)
		return
	case parsedURL.Hostname() == "":
		log.Debugf(`netenv: captive portal URL "%s" has no domain or IP`, portalURL)
		return
	default:
		// try to parse an IP
		portalIP := net.ParseIP(parsedURL.Hostname())
		if portalIP != nil {
			captivePortal.IP = portalIP
			captivePortal.Domain = SpecialCaptivePortalDomain
			return
		}

		// try to parse domain
		// ensure fqdn format
		domain := dns.Fqdn(parsedURL.Hostname())
		// check validity
		if !netutils.IsValidFqdn(domain) {
			log.Debugf(`netenv: captive portal domain/IP "%s" is invalid`, parsedURL.Hostname())
			return
		}
		// set domain
		captivePortal.Domain = domain
	}
}

// GetCaptivePortal returns the current captive portal. The returned struct must not be edited.
func GetCaptivePortal() *CaptivePortal {
	captivePortalLock.Lock()
	defer captivePortalLock.Unlock()

	return captivePortal
}

// ReportSuccessfulConnection hints the online status monitoring system that a connection attempt was successful.
func ReportSuccessfulConnection() {
	if !onlineStatusQuickCheck.IsSet() {
		triggerOnlineStatusInvestigation()
	}
}

// ReportFailedConnection hints the online status monitoring system that a connection attempt has failed. This function has extremely low overhead and may be called as much as wanted.
func ReportFailedConnection() {
	if onlineStatusQuickCheck.IsSet() {
		triggerOnlineStatusInvestigation()
	}
}

func triggerOnlineStatusInvestigation() {
	if onlineStatusInvestigationInProgress.SetToIf(false, true) {
		onlineStatusInvestigationWg.Add(1)
	}

	select {
	case onlineStatusInvestigationTrigger <- struct{}{}:
	default:
	}
}

func monitorOnlineStatus(ctx context.Context) error {
	triggerOnlineStatusInvestigation()
	for {
		// wait for trigger
		select {
		case <-ctx.Done():
			return nil
		case <-onlineStatusInvestigationTrigger:
		case <-getDynamicStatusTrigger():
		}

		// enable waiting
		if onlineStatusInvestigationInProgress.SetToIf(false, true) {
			onlineStatusInvestigationWg.Add(1)
		}

		checkOnlineStatus(ctx)

		// finished!
		onlineStatusInvestigationWg.Done()
		onlineStatusInvestigationInProgress.UnSet()
	}
}

func getDynamicStatusTrigger() <-chan time.Time {
	switch GetOnlineStatus() {
	case StatusOffline:
		return time.After(10 * time.Second)
	case StatusLimited, StatusPortal:
		return time.After(1 * time.Minute)
	case StatusSemiOnline:
		return time.After(5 * time.Minute)
	case StatusOnline:
		return nil
	default: // unknown status
		return time.After(5 * time.Minute)
	}
}

func checkOnlineStatus(ctx context.Context) {
	// TODO: implement more methods
	/*status, err := getConnectivityStateFromDbus()
	if err != nil {
		log.Warningf("environment: could not get connectivity: %s", err)
		setConnectivity(StatusUnknown)
		return StatusUnknown
	}*/

	// 1) check for addresses

	ipv4, ipv6, err := GetAssignedAddresses()
	if err != nil {
		log.Warningf("network: failed to get assigned network addresses: %s", err)
	} else {
		var lan bool
		for _, ip := range ipv4 {
			switch netutils.ClassifyIP(ip) {
			case netutils.SiteLocal:
				lan = true
			case netutils.Global:
				// we _are_ the Internet ;)
				updateOnlineStatus(StatusOnline, "", "global IPv4 interface detected")
				return
			}
		}
		for _, ip := range ipv6 {
			switch netutils.ClassifyIP(ip) {
			case netutils.SiteLocal, netutils.Global:
				// IPv6 global addresses are also used in local networks
				lan = true
			}
		}
		if !lan {
			updateOnlineStatus(StatusOffline, "", "no local or global interfaces detected")
			return
		}
	}

	// 2) try a http request

	// TODO: find (array of) alternatives to detectportal.firefox.com
	// TODO: find something about usage terms of detectportal.firefox.com

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		LocalAddr: getLocalAddr("tcp"),
		DualStack: true,
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext:        dialer.DialContext,
			DisableKeepAlives:  true,
			DisableCompression: true,
			WriteBufferSize:    1024,
			ReadBufferSize:     1024,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}

	request := (&http.Request{
		Method: "GET",
		URL:    parsedHTTPTestURL,
		Close:  true,
	}).WithContext(ctx)

	response, err := client.Do(request)
	if err != nil {
		updateOnlineStatus(StatusLimited, "", "http request failed")
		return
	}
	defer response.Body.Close()

	// check location
	portalURL, err := response.Location()
	if err == nil {
		updateOnlineStatus(StatusPortal, portalURL.String(), "http request succeeded with redirect")
		return
	}

	// read the body
	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Warningf("network: failed to read http body of captive portal testing response: %s", err)
		// assume we are online nonetheless
		updateOnlineStatus(StatusOnline, "", "http request succeeded, albeit failing later")
		return
	}

	// check body contents
	if strings.TrimSpace(string(data)) == HTTPExpectedContent {
		updateOnlineStatus(StatusOnline, "", "http request succeeded")
	} else {
		// something is interfering with the website content
		// this might be a weird captive portal, just direct the user there
		updateOnlineStatus(StatusPortal, "detectportal.firefox.com", "http request succeeded, response content not as expected")
	}
	// close the body now as we plan to reuse the http.Client
	response.Body.Close()

	// 3) try a https request
	dialer.LocalAddr = getLocalAddr("tcp")

	request = (&http.Request{
		Method: "HEAD",
		URL:    parsedHTTPSTestURL,
		Close:  true,
	}).WithContext(ctx)

	// only test if we can get the headers
	response, err = client.Do(request)
	if err != nil {
		// if we fail, something is really weird
		updateOnlineStatus(StatusSemiOnline, "", "http request failed to "+parsedHTTPSTestURL.String()+" with error "+err.Error())
		return
	}
	defer response.Body.Close()

	// finally
	updateOnlineStatus(StatusOnline, "", "all checks successful")
}
