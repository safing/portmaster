//go:build linux

package ivpn

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"sync/atomic"

	"github.com/ivpn/desktop-app/daemon/protocol/ivpnclient"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/spn/hub"
)

type platformSpecific struct {
	spnWgNftRuleHandle atomic.Int32                     // nft rule handle we registered for SPN compatibility with WireGuard
	spnWgIptRuleIP     atomic.Value                     // last WG local IP used for iptables fallback rule (string)
	spnHubInfo         atomic.Pointer[hub.Announcement] // last SPN hub info (hub.Info)
}

const (
	// NOTE: The nft table name is currently tied to IVPN's wg-quick setup.
	// If IVPN changes the WG interface naming, this constant may need adjustment.
	nftTableWgQuickIvpn     = "wg-quick-wgivpn"
	nftRuleCommentSPNCompat = "portmaster-spn-lo-rnat"
	spnSlitTunRouteTableID  = "717"
	spnSlitTunRulePriority  = "717"
)

func (i *InteropIvpn) spnConnectingHook(wc *mgr.WorkerCtx, homeHub hub.Announcement) (cancel bool, retErr error) {
	err := i.ensureWgCompatRule(wc)
	if err != nil {
		// Could happen, for example, if IVPN Client is paused
		wc.Warn(fmt.Sprintf("IVPN: failed to ensure WireGuard compatibility rule: %v", err))
	}

	err = i.ensureSpnHubBypassVpnRoutes(wc, &homeHub)
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: failed to ensure VPN and SPN tunnel routes: %v", err))
	}
	return false, nil
}

func (i *InteropIvpn) ensureSPNCompatibility(wc *mgr.WorkerCtx) error {
	err := i.ensureWgCompatRule(wc)
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: failed to ensure WireGuard compatibility rule: %v", err))
	}

	err = i.ensureSpnHubBypassVpnRoutes(wc, i.extra.spnHubInfo.Load())
	if err != nil {
		wc.Warn(fmt.Sprintf("IVPN: failed to ensure VPN and SPN tunnel routes: %v", err))
	}
	return nil
}

// SPN and SplitTunnel (ST) compatibility workaround for WireGuard kill-switch rules.
//
// WireGuard (wg-quick) installs a prerouting/raw kill-switch rule that drops
// packets destined to the WG local address when they arrive from non-WG interfaces.
// Portmaster SPN/ST reverse-NAT replies are delivered via loopback (iif lo) with a
// non-local source, which matches that drop pattern and breaks the TCP handshake
// (SYN-SENT/SYN-RECV).
//
// To preserve the kill-switch behavior while allowing SPN/ST reverse-NAT, Portmaster
// inserts a narrow exception rule before the wg-quick drop:
//   - nft path (preferred):
//     `iifname "lo" ip daddr <WG_LOCAL_IP> fib saddr type != local accept`
//   - iptables fallback (when nft is unavailable):
//     `-t raw -I PREROUTING 1 -d <WG_LOCAL_IP>/32 -i lo -m addrtype ! --src-type LOCAL -j ACCEPT`
//
// Rule lifecycle is managed here:
//   - Remove previously managed rule (nft/iptables) first.
//   - Recreate only when WireGuard is connected and SPN/ST is enabled.
func (i *InteropIvpn) ensureWgCompatRule(wc *mgr.WorkerCtx) error {
	status := i.getStatus()
	connectedInfo := status.connectedInfo

	nftPath, _ := exec.LookPath("nft")
	iptablesPath, _ := exec.LookPath("iptables")

	// Always clean previously managed rules first. This keeps behavior idempotent
	// across reconnects, interface IP changes, and SPN config toggles.
	if nftPath != "" {
		oldRuleHandle := i.extra.spnWgNftRuleHandle.Load()
		if oldRuleHandle != 0 {
			_ = exec.Command(nftPath, "delete", "rule", "ip", nftTableWgQuickIvpn, "preraw", "handle", strconv.Itoa(int(oldRuleHandle))).Run()
			i.extra.spnWgNftRuleHandle.Store(0)
		}
	}

	if iptablesPath != "" {
		if oldRuleIP, ok := i.extra.spnWgIptRuleIP.Load().(string); ok && oldRuleIP != "" {
			_ = exec.Command(
				iptablesPath,
				"-t", "raw",
				"-D", "PREROUTING",
				"-d", oldRuleIP+"/32",
				"-i", "lo",
				"-m", "addrtype", "!", "--src-type", "LOCAL",
				"-m", "comment", "--comment", nftRuleCommentSPNCompat,
				"-j", "ACCEPT",
			).Run()
			i.extra.spnWgIptRuleIP.Store("")
		}
	}

	if connectedInfo == nil || connectedInfo.VpnType != ivpnclient.WireGuard {
		return nil
	}

	vpnIP := net.ParseIP(connectedInfo.ClientIP)
	if vpnIP == nil {
		return nil
	}

	wgLocalIP := vpnIP.String()

	// If SPN not enabled -we do not need the rule
	cfgSpnEnabled := config.GetAsBool("spn/enable", false)
	cfgSplittunEnabled := config.GetAsBool("splittun/enable", false)
	if !cfgSpnEnabled() && !cfgSplittunEnabled() {
		return nil
	}

	if nftPath != "" {
		// Insert rule by executing command:
		// 		sudo nft --echo --json insert rule ip wg-quick-wgivpn preraw iifname "lo" ip daddr 1.2.3.4 fib saddr type != local accept comment "portmaster-spn-lo-rnat"
		out, err := exec.Command(nftPath, "--echo", "--json", "insert", "rule", "ip", nftTableWgQuickIvpn, "preraw",
			"iifname", "lo", "ip", "daddr", wgLocalIP, "fib", "saddr", "type", "!=", "local", "accept",
			"comment", nftRuleCommentSPNCompat).Output()
		if err != nil {
			return fmt.Errorf("failed to insert nft rule: %w", err)
		}

		handle, parseErr := parseNftInsertHandle(out)
		if parseErr != nil {
			return fmt.Errorf("failed to parse nft rule handle: %w", parseErr)
		}

		i.extra.spnWgNftRuleHandle.Store(int32(handle))
		wc.Debug(fmt.Sprintf("IVPN: Inserted nft SPN compatibility rule for WireGuard (handle %d, addr %s)", handle, wgLocalIP))
		return nil
	}

	if iptablesPath != "" {
		// Fallback for systems without nft where wg-quick uses iptables/raw rules.
		// Equivalent strict exception to allow SPN reverse-NAT loopback path.
		err := exec.Command(
			iptablesPath,
			"-t", "raw",
			"-I", "PREROUTING", "1",
			"-d", wgLocalIP+"/32",
			"-i", "lo",
			"-m", "addrtype", "!", "--src-type", "LOCAL",
			"-m", "comment", "--comment", nftRuleCommentSPNCompat,
			"-j", "ACCEPT",
		).Run()
		if err != nil {
			return fmt.Errorf("failed to insert iptables fallback rule: %w", err)
		}

		i.extra.spnWgIptRuleIP.Store(wgLocalIP)
		wc.Debug(fmt.Sprintf("IVPN: Inserted iptables SPN compatibility rule for WireGuard (addr %s)", wgLocalIP))
	}

	return nil
}

// parseNftInsertHandle extracts the rule handle from the JSON output of `nft --echo --json insert rule ...`.
func parseNftInsertHandle(data []byte) (int, error) {
	type ruleEntry struct {
		Rule struct {
			Handle int `json:"handle"`
		} `json:"rule"`
	}
	var out struct {
		Nftables []struct {
			Insert *ruleEntry `json:"insert,omitempty"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return 0, err
	}
	for _, entry := range out.Nftables {
		if entry.Insert != nil {
			return entry.Insert.Rule.Handle, nil
		}
	}
	return 0, fmt.Errorf("no rule entry found in nft output")
}

// ensureSpnHubBypassVpnRoutes keeps Linux policy routing in sync so
// traffic to the selected SPN hub is sent via the system default gateway, not
// through the active VPN tunnel.
//
// Why this is needed:
//   - When IVPN is connected, default routing points to the VPN interface.
//   - SPN hub control/data path must reach the hub directly on the non-VPN uplink.
//   - Without this rule/table setup, SPN hub traffic can be tunneled into VPN
//
// The function removes stale rules/routes from previous hub state, installs a
// dedicated routing table default route via the non-VPN gateway, and adds a
// high-priority destination rule for the current hub IP.
func (i *InteropIvpn) ensureSpnHubBypassVpnRoutes(wc *mgr.WorkerCtx, hubInfo *hub.Announcement) error {
	oldHubInfo := i.extra.spnHubInfo.Swap(hubInfo)

	ipPath, _ := exec.LookPath("ip")
	if ipPath == "" {
		return fmt.Errorf("ip command not found")
	}

	deleteRule := func(family, destination string) {
		if destination == "" {
			return
		}
		_ = exec.Command(ipPath, family, "rule", "del", "pref", spnSlitTunRulePriority,
			"to", destination, "lookup", spnSlitTunRouteTableID).Run()
	}

	// Clean up old rules for previous hub destination (if any).
	if oldHubInfo != nil {
		if oldHubInfo.IPv4 != nil {
			deleteRule("-4", oldHubInfo.IPv4.String()+"/32")
		}
		if oldHubInfo.IPv6 != nil {
			deleteRule("-6", oldHubInfo.IPv6.String()+"/128")
		}
		_ = exec.Command(ipPath, "-4", "route", "flush", "table", spnSlitTunRouteTableID).Run()
		_ = exec.Command(ipPath, "-6", "route", "flush", "table", spnSlitTunRouteTableID).Run()
	}

	// If VPN is not connected - we do not need to set up the rules.
	connectedInfo := i.getStatus().connectedInfo
	if connectedInfo == nil || connectedInfo.IsPaused {
		return nil
	}
	vpnInterfaceIP := net.ParseIP(connectedInfo.ClientIP)

	// If SPN not enabled - we do not need the rule
	// And erase stale info about the spnHub
	cfgSpnEnabled := config.GetAsBool("spn/enable", false)
	if !cfgSpnEnabled() || hubInfo == nil {
		i.extra.spnHubInfo.Store(nil)
		return nil
	}

	// Check the default gateway:
	// - the only one default gateway must be present
	// - the VPN connection gateway (interface) must be ignored
	var gw *netenv.GatewayInfo = nil
	gateways := netenv.GatewaysInfo()
	for idx := range gateways {
		g := &gateways[idx]
		if g.Mask == nil || g.IP == nil || g.Interface == "" {
			continue
		}
		// Mask: /0 - candidate default gateway.
		if ones, _ := g.Mask.Size(); ones == 0 {
			// Skip the gateway if it belongs to the VPN tunnel interface (heuristic by IP).
			if has, err := hasInterfaceIp(g.Interface, vpnInterfaceIP); err == nil && has {
				continue
			}
			// in case more than 1 default gateway exists, we can not be sure which one is correct
			if gw != nil {
				return fmt.Errorf("multiple default gateways found, unable to determine correct one")
			}
			gw = g
		}
	}

	if gw == nil {
		return fmt.Errorf("failed to find default gateway for SPN hub bypass route")
	}

	// Initialize route table

	family := "-4"
	hubRule := ""
	if gw.IP.To4() != nil {
		if err := exec.Command(ipPath, "-4", "route", "replace", "default",
			"via", gw.IP.String(), "dev", gw.Interface, "table", spnSlitTunRouteTableID).Run(); err != nil {
			return fmt.Errorf("failed to set IPv4 default route for SPN slit tunnel table: %w", err)
		}

		if hubInfo.IPv4 != nil {
			hubRule = hubInfo.IPv4.String() + "/32"
		}
	} else {
		family = "-6"
		if err := exec.Command(ipPath, "-6", "route", "replace", "default",
			"via", gw.IP.String(), "dev", gw.Interface, "table", spnSlitTunRouteTableID).Run(); err != nil {
			return fmt.Errorf("failed to set IPv6 default route for SPN slit tunnel table: %w", err)
		}

		if hubInfo.IPv6 != nil {
			hubRule = hubInfo.IPv6.String() + "/128"
		}
	}

	if hubRule == "" {
		return nil
	}

	// Remove potential stale rule for current hub destination before adding.
	deleteRule(family, hubRule)

	// Initialize rule to route SPN traffic
	if err := exec.Command(
		ipPath,
		family,
		"rule", "add",
		"pref", spnSlitTunRulePriority,
		"to", hubRule,
		"lookup", spnSlitTunRouteTableID,
	).Run(); err != nil {
		return fmt.Errorf("failed to add SPN hub policy route rule (%s): %w", hubRule, err)
	}

	wc.Debug(fmt.Sprintf("IVPN: Reconciled SPN hub route rule (%s -> table %s via %s dev %s)", hubRule, spnSlitTunRouteTableID, gw.IP.String(), gw.Interface))

	return nil
}

// hasInterfaceIp checks if the given IP address is assigned to the specified network interface.
func hasInterfaceIp(ifName string, ip net.IP) (bool, error) {
	iface, err := net.InterfaceByName(ifName)
	if err != nil {
		return false, err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return false, err
	}

	for _, addr := range addrs {
		var currentIP net.IP

		switch v := addr.(type) {
		case *net.IPNet:
			currentIP = v.IP
		case *net.IPAddr:
			currentIP = v.IP
		}

		if currentIP != nil && currentIP.Equal(ip) {
			return true, nil
		}
	}

	return false, nil
}
