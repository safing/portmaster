package firewall

import (
	"context"
	"net"
	"strings"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
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

	redirectTo_ipv4, redirectTo_ipv6, _ = getSplitTunVerdict(profile)
	return redirectTo_ipv4, redirectTo_ipv6
}

// GetSplitTunVerdictForConnection determines the split-tunneling verdict for a given connection.
// It returns the IP address to which traffic should be redirected, or nil if no redirection is needed (permit).
// If 'blockReason' is non-empty, the connection should be blocked for that reason.
func GetSplitTunVerdictForConnection(conn *network.Connection) (redirectToAddress *net.IP, blockReason string) {
	local_ipv4, local_ipv6, blockReason := getSplitTunVerdict(conn.Process().Profile())
	local_ip := local_ipv4
	if conn.IPVersion == packet.IPv6 {
		local_ip = local_ipv6
	}
	return local_ip, blockReason
}

// getSplitTunVerdict determines the split-tunneling verdict for a given process ID.
// It returns the IP addresses to which IPv4 and IPv6 traffic should be redirected, or nil if no redirection is needed (permit).
// If 'blockReason' is non-empty, the connection should be blocked for that reason.
func getSplitTunVerdict(profile *profile.LayeredProfile) (redirectTo_ipv4 *net.IP, redirectTo_ipv6 *net.IP, blockReason string) {
	redirectTo_ipv4 = nil // nil means no redirect (permit) by default
	redirectTo_ipv6 = nil // nil means no redirect (permit) by default
	blockReason = ""      // empty means no block by default

	if profile == nil {
		return
	}

	interfaceIdentifier := strings.TrimSpace(profile.SplitTunInterface())
	if len(interfaceIdentifier) == 0 {
		return // No split-tunneling interface set.
	}

	redirectTo_ipv4, redirectTo_ipv6 = netenv.GetLocalInterfaceIPs(interfaceIdentifier)
	if redirectTo_ipv4 == nil && redirectTo_ipv6 == nil {
		if profile.SplitTunBlockOnFallback() {
			blockReason = "split-tunneling: blocked according to 'Split Tunnel: Block On Fallback' option"
		}
		return
	}

	return redirectTo_ipv4, redirectTo_ipv6, blockReason
}
