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
)

type platformSpecific struct {
	wgNftRuleHandle atomic.Int64 // nft rule handle we registered for split tunnel compatibility with WireGuard
	wgIptRuleIP     atomic.Value // last WG local IP used for iptables fallback rule (string)
}

const (
	// NOTE: The nft table name is currently tied to IVPN's wg-quick setup.
	// If IVPN changes the WG interface naming, this constant may need adjustment.
	nftTableWgQuickIvpn       = "wg-quick-wgivpn"
	wgKillswitchBypassComment = "portmaster-wg-ks-bypass"
)

// reconcileCompatibilityState reconciles the WireGuard firewall rule to
// maintain SplitTunnel compatibility with the current IVPN connection state.
func (i *InteropIvpn) reconcileCompatibilityState(wc *mgr.WorkerCtx) {
	// Ensure WireGuard-specific firewall rule is in place or removed as needed based on current VPN and ST state.
	err := i.ensureWgCompatRule(wc)
	if err != nil {
		// Could happen, for example, if IVPN Client is paused
		wc.Warn(fmt.Sprintf("IVPN: failed to ensure WireGuard compatibility rule: %v", err))
	}
}

// SplitTunnel (ST) compatibility workaround for WireGuard kill-switch rules.
//
// WireGuard (wg-quick) installs a prerouting/raw kill-switch rule that drops
// packets destined to the WG local address when they arrive from non-WG interfaces.
// Portmaster ST reverse-NAT replies are delivered via loopback (iif lo) with a
// non-local source, which matches that drop pattern and breaks the TCP handshake
// (SYN-SENT/SYN-RECV).
//
// To preserve the kill-switch behavior while allowing ST reverse-NAT, Portmaster
// inserts a narrow exception rule before the wg-quick drop:
//   - nft path (when wg-quick-wgivpn table exists, i.e. non-snap IVPN):
//     `iifname "lo" ip daddr <WG_LOCAL_IP> fib saddr type != local accept`
//   - iptables path (snap IVPN or systems without nft):
//     `-t raw -I PREROUTING 1 -d <WG_LOCAL_IP>/32 -i lo -m addrtype ! --src-type LOCAL -j ACCEPT`
//
// Rule lifecycle is managed here:
//   - Remove previously managed rule (nft/iptables) first.
//   - Recreate only when WireGuard is connected and ST is enabled.
func (i *InteropIvpn) ensureWgCompatRule(wc *mgr.WorkerCtx) error {
	status := i.getStatus()
	connectedInfo := status.connectedInfo

	nftPath, _ := exec.LookPath("nft")
	iptablesPath, _ := exec.LookPath("iptables")

	// Always clean previously managed rules first. This keeps behavior idempotent
	// across reconnects, interface IP changes, and ST config toggles.
	if nftPath != "" {
		oldRuleHandle := i.extra.wgNftRuleHandle.Load()
		if oldRuleHandle != 0 {
			_ = exec.Command(nftPath, "delete", "rule", "ip", nftTableWgQuickIvpn, "preraw", "handle", strconv.FormatInt(oldRuleHandle, 10)).Run()
			i.extra.wgNftRuleHandle.Store(0)
		}
	}

	if iptablesPath != "" {
		if oldRuleIP, ok := i.extra.wgIptRuleIP.Load().(string); ok && oldRuleIP != "" {
			_ = exec.Command(
				iptablesPath,
				"-t", "raw",
				"-D", "PREROUTING",
				"-d", oldRuleIP+"/32",
				"-i", "lo",
				"-m", "addrtype", "!", "--src-type", "LOCAL",
				"-m", "comment", "--comment", wgKillswitchBypassComment,
				"-j", "ACCEPT",
			).Run()
			i.extra.wgIptRuleIP.Store("")
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

	// If split tunneling is not enabled - we do not need the rule
	cfgSplittunEnabled := config.GetAsBool("splittun/enable", false)
	if !cfgSplittunEnabled() {
		return nil
	}

	if nftPath != "" && nftTableExists(nftPath, nftTableWgQuickIvpn) {
		// Insert rule by executing command:
		// 		sudo nft --echo --json insert rule ip wg-quick-wgivpn preraw iifname "lo" ip daddr 1.2.3.4 fib saddr type != local accept comment "portmaster-wg-ks-bypass"
		out, err := exec.Command(nftPath, "--echo", "--json", "insert", "rule", "ip", nftTableWgQuickIvpn, "preraw",
			"iifname", "lo", "ip", "daddr", wgLocalIP, "fib", "saddr", "type", "!=", "local", "accept",
			"comment", wgKillswitchBypassComment).Output()
		if err != nil {
			return fmt.Errorf("failed to insert nft rule: %w", err)
		}

		handle, parseErr := parseNftInsertHandle(out)
		if parseErr != nil {
			return fmt.Errorf("failed to parse nft rule handle: %w", parseErr)
		}

		i.extra.wgNftRuleHandle.Store(int64(handle))
		wc.Debug(fmt.Sprintf("IVPN: Inserted nft split tunnel compatibility rule for WireGuard (handle %d, addr %s)", handle, wgLocalIP))
		return nil
	}

	if iptablesPath != "" {
		// Fallback for systems without nft where wg-quick uses iptables/raw rules.
		// Equivalent strict exception to allow ST reverse-NAT loopback path.
		err := exec.Command(
			iptablesPath,
			"-t", "raw",
			"-I", "PREROUTING", "1",
			"-d", wgLocalIP+"/32",
			"-i", "lo",
			"-m", "addrtype", "!", "--src-type", "LOCAL",
			"-m", "comment", "--comment", wgKillswitchBypassComment,
			"-j", "ACCEPT",
		).Run()
		if err != nil {
			return fmt.Errorf("failed to insert iptables fallback rule: %w", err)
		}

		i.extra.wgIptRuleIP.Store(wgLocalIP)
		wc.Debug(fmt.Sprintf("IVPN: Inserted iptables split tunnel compatibility rule for WireGuard (addr %s)", wgLocalIP))
	}

	return nil
}

// nftTableExists reports whether an nft table with the given name exists in the "ip" family.
func nftTableExists(nftPath, table string) bool {
	return exec.Command(nftPath, "list", "table", "ip", table).Run() == nil
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
