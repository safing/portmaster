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
// WireGuard (wg-quick) installs a prerouting/raw kill-switch nft rule that drops packets
// destined to the WG local address when they arrive from non-WG interfaces.
// Portmaster SPN reverse-NAT replies are delivered via loopback (iif lo) with a non-local
// source, which matches that drop pattern and breaks the TCP handshake (SYN-SENT/SYN-RECV).
// Insert an allow rule before the wg-quick drop to permit this specific loopback reverse path.
// More info:
//   - check WG drop rule:
//     `sudo nft list chain ip wg-quick-wgivpn preraw`
//     you will see something like this:
//     `iifname != "wgivpn" ip daddr <WG_LOCAL_IP> fib saddr type != local drop`
//   - the rule we need to insert before that would be:
//     `iifname "lo" ip daddr <WG_LOCAL_IP> fib saddr type != local accept comment "portmaster-spn-lo-rnat"`
//
// NOTE! here we use some constant values:
//   - wg-quick-wgivpn: IVPN Client creates a WireGuard interface named "wgivpn", and wg-quick creates a chain named "wg-quick-wgivpn" for it.
//     If IVPN changes the interface name, this will need to be updated.
func (i *InteropIvpn) reconcileWgCompatRule(wc *mgr.WorkerCtx) error {
	status := i.getStatus()
	connectedInfo := status.connectedInfo

	if connectedInfo == nil || connectedInfo.VpnType != ivpnclient.WireGuard {
		i.extra.spnWgNftRuleHandle.Store(0)
		return nil
	}

	vpnIP := net.ParseIP(connectedInfo.ClientIP)
	if vpnIP == nil {
		return nil
	}

	wgLocalIP := vpnIP.String()

	nftPath, err := exec.LookPath("nft")
	if err != nil {
		return nil // silently return if 'nft' is not available
	}

	// Remove OLD existing rule (if any)
	// 		delete rule by handle: `sudo nft delete rule ip wg-quick-wgivpn preraw handle <handle>`
	oldRuleHandle := i.extra.spnWgNftRuleHandle.Load()
	if oldRuleHandle != 0 {
		_ = exec.Command(nftPath, "delete", "rule", "ip", nftTableWgQuickIvpn, "preraw", "handle", strconv.Itoa(int(oldRuleHandle))).Run()
		i.extra.spnWgNftRuleHandle.Store(0)
	}

	// If SPN not enabled -we do not need the rule
	if !i.cfgSpnEnabled() {
		return nil
	}

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
