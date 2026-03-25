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
	"github.com/safing/portmaster/service/mgr"
)

type platformSpecific struct {
	spnWgNftRuleHandle atomic.Int32 // nft rule handle we registered for SPN compatibility with WireGuard
	spnWgIptRuleIP     atomic.Value // last WG local IP used for iptables fallback rule (string)
}

const (
	nftTableWgQuickIvpn     = "wg-quick-wgivpn"
	nftRuleCommentSPNCompat = "portmaster-spn-lo-rnat"
)

func (i *InteropIvpn) ensureSPNCompatibility(wc *mgr.WorkerCtx) error {
	err := i.reconcileWgCompatRule(wc)
	if err != nil {
		return fmt.Errorf("failed to reconcile WireGuard compatibility rule: %w", err)
	}
	return nil
}

// SPN compatibility workaround for WireGuard kill-switch rules.
//
// WireGuard (wg-quick) installs a prerouting/raw kill-switch rule that drops
// packets destined to the WG local address when they arrive from non-WG interfaces.
// Portmaster SPN reverse-NAT replies are delivered via loopback (iif lo) with a
// non-local source, which matches that drop pattern and breaks the TCP handshake
// (SYN-SENT/SYN-RECV).
//
// To preserve the kill-switch behavior while allowing SPN reverse-NAT, Portmaster
// inserts a narrow exception rule before the wg-quick drop:
//   - nft path (preferred):
//     `iifname "lo" ip daddr <WG_LOCAL_IP> fib saddr type != local accept`
//   - iptables fallback (when nft is unavailable):
//     `-t raw -I PREROUTING 1 -d <WG_LOCAL_IP>/32 -i lo -m addrtype ! --src-type LOCAL -j ACCEPT`
//
// Rule lifecycle is managed here:
//   - Remove previously managed rule (nft/iptables) first.
//   - Recreate only when WireGuard is connected and SPN is enabled.
//
// NOTE: The nft table/chain name is currently tied to IVPN's wg-quick setup.
// If IVPN changes the WG interface naming, this constant may need adjustment.
func (i *InteropIvpn) reconcileWgCompatRule(wc *mgr.WorkerCtx) error {
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
	if !i.cfgSpnEnabled() {
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
