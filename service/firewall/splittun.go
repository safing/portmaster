package firewall

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
)

func handleRedirectRequest(req packet.BindRequest) {
	// Get split-tunneling verdict
	redirectTo_ipv4, redirectTo_ipv6 := getSplitTunVerdictForPid(req.ProcessID())

	// Send response (redirection verdict) back
	if err := req.ReplySplitTunnel(redirectTo_ipv4, redirectTo_ipv6); err != nil {
		log.Errorf("failed to reply to redirect request: %s (pid %d)", err, req.ProcessID())
	}
}

// GetSplitTunVerdict determines the split-tunneling verdict for a given process ID.
// It returns the IP addresses to which IPv4 and IPv6 traffic should be redirected, or nil if no redirection is needed (permit).
func getSplitTunVerdictForPid(processID uint64) (redirectTo_ipv4 *net.IP, redirectTo_ipv6 *net.IP) {
	proc, err := process.GetProcessWithProfile(context.Background(), int(processID))
	if err != nil {
		log.Errorf("splittun verdict: failed to get process for PID %d: %s", processID, err)
		return nil, nil
	}

	profile := proc.Profile()
	if profile == nil {
		log.Warningf("splittun verdict: process PID %d has no profile, cannot apply split-tunneling", processID)
		return nil, nil
	}

	return getSplitTunVerdict(profile)
}

// GetSplitTunVerdictForConnection determines the split-tunneling verdict for a given connection.
// It returns the IP address to which traffic should be redirected, or nil if no redirection is needed (permit).
func GetSplitTunVerdictForConnection(conn *network.Connection) (redirectToAddress *net.IP) {
	local_ipv4, local_ipv6 := getSplitTunVerdict(conn.Process().Profile())
	local_ip := local_ipv4
	if conn.IPVersion == packet.IPv6 {
		local_ip = local_ipv6
	}
	return local_ip
}

// getSplitTunVerdict determines the split-tunneling verdict for a given process ID.
// It returns the IP addresses to which IPv4 and IPv6 traffic should be redirected, or nil if no redirection is needed (permit).
func getSplitTunVerdict(profile *profile.LayeredProfile) (redirectTo_ipv4 *net.IP, redirectTo_ipv6 *net.IP) {
	redirectTo_ipv4 = nil // nil means no redirect (permit) by default
	redirectTo_ipv6 = nil // nil means no redirect (permit) by default

	if profile == nil {
		return
	}

	ifIP := strings.TrimSpace(profile.SplitTunInterface())
	if len(ifIP) == 0 {
		return // No split-tunneling interface set.
	}

	// TODO: Implement better way of determining the IP address of the interface.

	ip := net.ParseIP(ifIP)
	if ip == nil {
		log.Warningf("splittun verdict: failed to parse split-tunneling interface IP '%s' for profile '%s'", ifIP, profile.LocalProfile().Name)
		return
	}

	fmt.Printf("REDIRECT: %s to '%v'\n", profile.LocalProfile().Name, ip)

	if ip.To4() != nil {
		redirectTo_ipv4 = &ip
	} else if ip.To16() != nil {
		redirectTo_ipv6 = &ip
	}
	return
}
