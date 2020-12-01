package netenv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portbase/database"

	"github.com/safing/portbase/notifications"

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
var (
	PortalTestIP  = net.IPv4(192, 0, 2, 1)
	PortalTestURL = fmt.Sprintf("http://%s/", PortalTestIP)

	DNSTestDomain     = "one.one.one.one."
	DNSTestExpectedIP = net.IPv4(1, 1, 1, 1)

	// SpecialCaptivePortalDomain is the domain name used to point to the detected captive portal IP
	// or the captive portal test IP. The default value should be overridden by the resolver package,
	// which defines the custom internal domain name to use.
	SpecialCaptivePortalDomain = "captiveportal.invalid."
)

var (
	parsedPortalTestURL *url.URL
)

func prepOnlineStatus() (err error) {
	parsedPortalTestURL, err = url.Parse(PortalTestURL)
	return err
}

// IsConnectivityDomain checks whether the given domain (fqdn) is used for any
// connectivity related network connections and should always be resolved using
// the network assigned DNS server.
func IsConnectivityDomain(domain string) bool {
	if domain == "" {
		return false
	}

	switch domain {
	case SpecialCaptivePortalDomain,
		"one.one.one.one.", // Internal DNS Check

		// Windows
		"dns.msftncsi.com.", // DNS Check
		"msftncsi.com.",     // Older
		"www.msftncsi.com.",
		"microsoftconnecttest.com.", // Newer
		"www.microsoftconnecttest.com.",
		"ipv6.microsoftconnecttest.com.",
		// https://de.wikipedia.org/wiki/Captive_Portal
		// https://docs.microsoft.com/en-us/windows-hardware/drivers/mobilebroadband/captive-portals
		// TODO: read value from registry: HKLM:\SYSTEM\CurrentControlSet\Services\NlaSvc\Parameters\Internet

		// Apple
		"captive.apple.com.",
		// https://de.wikipedia.org/wiki/Captive_Portal

		// Linux
		"connectivity-check.ubuntu.com.", // Ubuntu
		"nmcheck.gnome.org.",             // Gnome DE
		"network-test.debian.org.",       // Debian
		// There are probably a lot more domains for all the Linux Distro/DE Variants. Please raise issues and/or submit PRs!
		// https://github.com/solus-project/budgie-desktop/issues/807
		// https://www.lguruprasad.in/blog/2015/07/21/enabling-captive-portal-detection-in-gnome-3-14-on-debian-jessie/
		// TODO: read value from NetworkManager config: /etc/NetworkManager/conf.d/*.conf

		// Android
		"connectivitycheck.gstatic.com.",
		// https://de.wikipedia.org/wiki/Captive_Portal

		// Other
		"neverssl.com.",             // Common Community Service
		"detectportal.firefox.com.": // Firefox

		return true
	}

	// Check for captive portal domain.
	captivePortal := GetCaptivePortal()
	if captivePortal.Domain != "" &&
		domain == captivePortal.Domain {
		return true
	}

	return false
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

	captivePortal             = &CaptivePortal{}
	captivePortalLock         sync.Mutex
	captivePortalNotification *notifications.Notification
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

func updateOnlineStatus(status OnlineStatus, portalURL *url.URL, comment string) {
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
	if status == StatusOffline || portalURL != nil {
		setCaptivePortal(portalURL)
	} else if status == StatusOnline {
		cleanUpPortalNotification()
	}

	// trigger event
	if changed {
		module.TriggerEvent(OnlineStatusChangedEvent, nil)
		if status == StatusPortal {
			log.Infof(`netenv: setting online status to %s at "%s" (%s)`, status, portalURL, comment)
		} else {
			log.Infof("netenv: setting online status to %s (%s)", status, comment)
		}
		triggerNetworkChangeCheck()
	}
}

func setCaptivePortal(portalURL *url.URL) {
	captivePortalLock.Lock()
	defer captivePortalLock.Unlock()

	// delete
	if portalURL == nil {
		captivePortal = &CaptivePortal{}
		cleanUpPortalNotification()
		return
	}

	// return if unchanged
	if portalURL.String() == captivePortal.URL {
		return
	}

	// set
	captivePortal = &CaptivePortal{
		URL: portalURL.String(),
	}
	portalIP := net.ParseIP(portalURL.Hostname())
	if portalIP != nil {
		captivePortal.IP = portalIP
		captivePortal.Domain = SpecialCaptivePortalDomain
	} else {
		captivePortal.Domain = portalURL.Hostname()
	}

	// notify
	cleanUpPortalNotification()
	captivePortalNotification = notifications.Notify(&notifications.Notification{
		EventID:  "netenv:captive-portal",
		Type:     notifications.Info,
		Title:    "Captive Portal",
		Category: "Core",
		Message: fmt.Sprintf(
			"Portmaster detected a captive portal at %s",
			captivePortal.Domain,
		),
		EventData: captivePortal,
	})
}

func cleanUpPortalNotification() {
	if captivePortalNotification != nil {
		err := captivePortalNotification.Delete()
		if err != nil && err != database.ErrNotFound {
			log.Warningf("netenv: failed to delete old captive portal notification: %s", err)
		}
		captivePortalNotification = nil
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
		return time.After(5 * time.Second)
	case StatusLimited, StatusPortal:
		return time.After(10 * time.Second)
	case StatusSemiOnline:
		return time.After(1 * time.Minute)
	case StatusOnline:
		return nil
	case StatusUnknown:
		return time.After(2 * time.Second)
	default: // other unknown status
		return time.After(1 * time.Minute)
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
				updateOnlineStatus(StatusOnline, nil, "global IPv4 interface detected")
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
			updateOnlineStatus(StatusOffline, nil, "no local or global interfaces detected")
			return
		}
	}

	// 2) try a http request

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		LocalAddr: getLocalAddr("tcp"),
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
		Timeout: 1 * time.Second,
	}

	request := (&http.Request{
		Method: "GET",
		URL:    parsedPortalTestURL,
		Close:  true,
	}).WithContext(ctx)

	response, err := client.Do(request)
	if err != nil {
		nErr, ok := err.(net.Error)
		if !ok || !nErr.Timeout() {
			// Timeout is the expected error when there is no portal
			log.Debugf("netenv: http portal test failed: %s", err)
			// TODO: discern between errors to detect StatusLimited
		}
	} else {
		defer response.Body.Close()
		// Got a response, something is messing with the request

		// check location
		portalURL, err := response.Location()
		if err == nil {
			updateOnlineStatus(StatusPortal, portalURL, "portal test request succeeded with redirect")
			return
		}

		// direct response
		if response.StatusCode == 200 {
			updateOnlineStatus(StatusPortal, &url.URL{
				Scheme: "http",
				Host:   SpecialCaptivePortalDomain,
				Path:   "/",
			}, "portal test request succeeded")
			return
		}

		log.Debugf("netenv: unexpected http portal test response code: %d", response.StatusCode)
		// other responses are undefined, continue with next test
	}

	// 3) resolve a query

	// make DNS request
	ips, err := net.LookupIP(DNSTestDomain)
	if err != nil {
		updateOnlineStatus(StatusSemiOnline, nil, "dns check query failed")
		return
	}
	// check for expected response
	for _, ip := range ips {
		if ip.Equal(DNSTestExpectedIP) {
			updateOnlineStatus(StatusOnline, nil, "all checks passed")
			return
		}
	}
	// unexpected response
	updateOnlineStatus(StatusSemiOnline, nil, "dns check query response mismatched")
}
