package netenv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/updates"
)

// OnlineStatus represent a state of connectivity to the Internet.
type OnlineStatus uint8

// Online Status Values.
const (
	StatusUnknown    OnlineStatus = 0
	StatusOffline    OnlineStatus = 1
	StatusLimited    OnlineStatus = 2 // local network only
	StatusPortal     OnlineStatus = 3 // there seems to be an internet connection, but we are being intercepted, possibly by a captive portal
	StatusSemiOnline OnlineStatus = 4 // we seem to online, but without full connectivity
	StatusOnline     OnlineStatus = 5
)

// Online Status and Resolver.
var (
	PortalTestIP  = net.IPv4(192, 0, 2, 1)
	PortalTestURL = fmt.Sprintf("http://%s/", PortalTestIP)

	// IP address -> 100.127.247.245 is a special ip used by the android VPN service. Must be ignored during online check.
	IgnoreIPsInOnlineStatusCheck = []net.IP{net.IPv4(100, 127, 247, 245)}

	DNSTestDomain     = "online-check.safing.io."
	DNSTestExpectedIP = net.IPv4(0, 65, 67, 75) // Ascii: \0ACK
	DNSTestQueryFunc  func(ctx context.Context, fdqn string) (ips []net.IP, ok bool, err error)

	ConnectedToSPN = abool.New()
	ConnectedToDNS = abool.New()

	// SpecialCaptivePortalDomain is the domain name used to point to the detected captive portal IP
	// or the captive portal test IP. The default value should be overridden by the resolver package,
	// which defines the custom internal domain name to use.
	SpecialCaptivePortalDomain = "captiveportal.invalid."

	// ConnectivityDomains holds all connectivity domains. This slice must not be modified.
	ConnectivityDomains = []string{
		SpecialCaptivePortalDomain,

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
		"204.pop-os.org.",                // Pop OS
		"conncheck.opensuse.org.",        // OpenSUSE
		"ping.archlinux.org",             // Arch
		// There are probably a lot more domains for all the Linux Distro/DE Variants. Please raise issues and/or submit PRs!
		// https://github.com/solus-project/budgie-desktop/issues/807
		// https://www.lguruprasad.in/blog/2015/07/21/enabling-captive-portal-detection-in-gnome-3-14-on-debian-jessie/
		// TODO: read value from NetworkManager config: /etc/NetworkManager/conf.d/*.conf

		// Android
		"connectivitycheck.gstatic.com.",
		// https://de.wikipedia.org/wiki/Captive_Portal

		// Other
		"neverssl.com.",             // Common Community Service
		"detectportal.firefox.com.", // Firefox
	}

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

	for _, connectivityDomain := range ConnectivityDomains {
		if domain == connectivityDomain {
			return true
		}
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
	case StatusUnknown:
		fallthrough
	default:
		return "Unknown"
	}
}

var (
	onlineStatus           *int32
	onlineStatusQuickCheck = abool.NewBool(false)

	onlineStatusInvestigationTrigger    = make(chan struct{}, 1)
	onlineStatusInvestigationInProgress = abool.NewBool(false)
	onlineStatusInvestigationWg         sync.WaitGroup
	onlineStatusNotification            *notifications.Notification

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

// CheckAndGetOnlineStatus triggers a new online status check and returns the result.
func CheckAndGetOnlineStatus() OnlineStatus {
	// trigger new investigation
	TriggerOnlineStatusInvestigation()
	// wait for completion
	onlineStatusInvestigationWg.Wait()
	// return current status
	return GetOnlineStatus()
}

func updateOnlineStatus(status OnlineStatus, portalURL *url.URL, comment string) {
	changed := false

	// Update online status.
	currentStatus := atomic.LoadInt32(onlineStatus)
	if status != OnlineStatus(currentStatus) && atomic.CompareAndSwapInt32(onlineStatus, currentStatus, int32(status)) {
		// status changed!
		onlineStatusQuickCheck.SetTo(
			status == StatusOnline || status == StatusSemiOnline,
		)
		changed = true
	}

	// Update captive portal.
	setCaptivePortal(portalURL)

	// Trigger events.
	if changed {
		module.EventOnlineStatusChange.Submit(status)
		if status == StatusPortal {
			log.Infof(`netenv: setting online status to %s at "%s" (%s)`, status, portalURL, comment)
		} else {
			log.Infof("netenv: setting online status to %s (%s)", status, comment)
		}
		TriggerNetworkChangeCheck()

		// Notify user.
		notifyOnlineStatus(status)

		// Trigger update check when coming (semi) online.
		if Online() {
			_ = updates.TriggerUpdate(false, false)
		}
	}
}

func notifyOnlineStatus(status OnlineStatus) {
	var eventID, title, message string

	// Check if status is worth notifying.
	switch status { //nolint:exhaustive // Checking for selection only.
	case StatusOffline:
		eventID = "netenv:online-status:offline"
		title = "Device is Offline"
		message = "Portmaster did not detect any network connectivity."
	case StatusLimited:
		eventID = "netenv:online-status:limited"
		title = "Limited network connectivity."
		message = "Portmaster did detect local network connectivity, but could not detect connectivity to the Internet."
	default:
		// Delete notification, if present.
		if onlineStatusNotification != nil {
			onlineStatusNotification.Delete()
			onlineStatusNotification = nil
		}
		return
	}

	// Update notification if not present or online status changed.
	switch {
	case onlineStatusNotification == nil:
		// Continue creating new notification.
	case onlineStatusNotification.EventID == eventID:
		// Notification stays the same, stick with the old one.
		return
	default:
		// Delete old notification before triggering updated one.
		onlineStatusNotification.Delete()
	}

	// Create update status notification.
	onlineStatusNotification = notifications.Notify(&notifications.Notification{
		EventID: eventID,
		Type:    notifications.Info,
		Title:   title,
		Message: message,
	})
}

func setCaptivePortal(portalURL *url.URL) {
	captivePortalLock.Lock()
	defer captivePortalLock.Unlock()

	// Delete captive portal if no url is supplied.
	if portalURL == nil {
		captivePortal = &CaptivePortal{}
		if captivePortalNotification != nil {
			captivePortalNotification.Delete()
			captivePortalNotification = nil
		}
		return
	}

	// Only set captive portal once per detection.
	if captivePortal.URL != "" {
		return
	}

	// Compile captive portal data.
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

	// Notify user about portal.
	captivePortalNotification = notifications.Notify(&notifications.Notification{
		EventID:      "netenv:captive-portal",
		Type:         notifications.Info,
		Title:        "Captive Portal Detected",
		Message:      "The Portmaster detected a captive portal. You might experience limited network connectivity until the portal is handled.",
		ShowOnSystem: true,
		EventData:    captivePortal,
		AvailableActions: []*notifications.Action{
			{
				Text:    "Open Portal",
				Type:    notifications.ActionTypeOpenURL,
				Payload: captivePortal.URL,
			},
			{
				ID:   "ack",
				Text: "Ignore",
			},
		},
	})
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
		TriggerOnlineStatusInvestigation()
	}
}

// ReportFailedConnection hints the online status monitoring system that a connection attempt has failed. This function has extremely low overhead and may be called as much as wanted.
func ReportFailedConnection() {
	if onlineStatusQuickCheck.IsSet() {
		TriggerOnlineStatusInvestigation()
	}
}

// TriggerOnlineStatusInvestigation manually triggers the online status check.
// It will not trigger it again, if it is already in progress.
func TriggerOnlineStatusInvestigation() {
	if onlineStatusInvestigationInProgress.SetToIf(false, true) {
		onlineStatusInvestigationWg.Add(1)
	}

	select {
	case onlineStatusInvestigationTrigger <- struct{}{}:
	default:
	}
}

func monitorOnlineStatus(ctx *mgr.WorkerCtx) error {
	TriggerOnlineStatusInvestigation()
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

		checkOnlineStatus(ctx.Ctx())

		// finished!
		onlineStatusInvestigationWg.Done()
		onlineStatusInvestigationInProgress.UnSet()
	}
}

func getDynamicStatusTrigger() <-chan time.Time {
	switch GetOnlineStatus() {
	case StatusOffline:
		// Will also be triggered by network change.
		return time.After(10 * time.Second)
	case StatusLimited, StatusPortal:
		// Change will not be detected otherwise, but impact is minor.
		return time.After(5 * time.Second)
	case StatusSemiOnline:
		// Very small impact.
		return time.After(60 * time.Second)
	case StatusOnline:
		// Don't check until resolver reports problems.
		return nil
	case StatusUnknown:
		fallthrough
	default:
		return time.After(5 * time.Minute)
	}
}

func ipInList(list []net.IP, ip net.IP) bool {
	for _, ignoreIP := range list {
		if ignoreIP.Equal(ip) {
			return true
		}
	}
	return false
}

func checkOnlineStatus(ctx context.Context) {
	// TODO: implement more methods
	/*status, err := getConnectivityStateFromDbus()
	if err != nil {
		log.Warningf("environment: could not get connectivity: %s", err)
		setConnectivity(StatusUnknown)
		return StatusUnknown
	}*/

	// 0) check if connected to SPN and/or DNS.

	if ConnectedToSPN.IsSet() {
		updateOnlineStatus(StatusOnline, nil, "connected to SPN")
		return
	}

	if ConnectedToDNS.IsSet() {
		updateOnlineStatus(StatusOnline, nil, "connected to DNS")
		return
	}

	// 1) check for addresses

	ipv4, ipv6, err := GetAssignedAddresses()
	if err != nil {
		log.Warningf("netenv: failed to get assigned network addresses: %s", err)
	} else {
		var lan bool

		for _, ip := range ipv4 {
			// Ignore IP if it is in the online check ignore list.
			if ipInList(IgnoreIPsInOnlineStatusCheck, ip) {
				continue
			}

			switch netutils.GetIPScope(ip) { //nolint:exhaustive // Checking to specific values only.
			case netutils.SiteLocal:
				lan = true
			case netutils.Global:
				// we _are_ the Internet ;)
				updateOnlineStatus(StatusOnline, nil, "global IPv4 interface detected")
				return
			}
		}

		for _, ip := range ipv6 {
			// Ignore IP if it is in the online check ignore list.
			if ipInList(IgnoreIPsInOnlineStatusCheck, ip) {
				continue
			}

			switch netutils.GetIPScope(ip) { //nolint:exhaustive // Checking to specific values only.
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
		Method: http.MethodGet,
		URL:    parsedPortalTestURL,
		Close:  true,
	}).WithContext(ctx)

	response, err := client.Do(request)
	if err != nil {
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			// Timeout is the expected error when there is no portal
			log.Debugf("netenv: http portal test failed: %s", err)
			// TODO: discern between errors to detect StatusLimited
		}
	} else {
		defer func() {
			_ = response.Body.Close()
		}()
		// Got a response, something is messing with the request

		// check location
		portalURL, err := response.Location()
		if err == nil {
			updateOnlineStatus(StatusPortal, portalURL, "portal test request succeeded with redirect")
			return
		}

		// direct response
		if response.StatusCode == http.StatusOK {
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

	// Check if we can resolve the dns check domain.
	if DNSTestQueryFunc == nil {
		updateOnlineStatus(StatusOnline, nil, "all checks passed, dns query check disabled")
		return
	}
	ips, ok, err := DNSTestQueryFunc(ctx, DNSTestDomain)
	switch {
	case ok && err != nil:
		updateOnlineStatus(StatusOnline, nil, fmt.Sprintf(
			"all checks passed, acceptable result for dns query check: %s",
			err,
		))
	case ok && len(ips) >= 1 && ips[0].Equal(DNSTestExpectedIP):
		updateOnlineStatus(StatusOnline, nil, "all checks passed")
	case ok && len(ips) >= 1:
		log.Warningf("netenv: dns query check response mismatched: got %s", ips[0])
		updateOnlineStatus(StatusOnline, nil, "all checks passed, dns query check response mismatched")
	case ok:
		log.Warningf("netenv: dns query check response mismatched: empty response")
		updateOnlineStatus(StatusOnline, nil, "all checks passed, dns query check response was empty")
	default:
		log.Warningf("netenv: dns query check failed: %s", err)
		updateOnlineStatus(StatusOffline, nil, "dns query check failed")
	}
}
