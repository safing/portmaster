package interception

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coreos/go-iptables/iptables"
	"github.com/hashicorp/go-multierror"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/firewall/interception/nfq"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/packet"
)

var (
	v4chains []string
	v4rules  []string
	v4once   []string

	v6chains []string
	v6rules  []string
	v6once   []string

	out4Queue nfQueue
	in4Queue  nfQueue
	out6Queue nfQueue
	in6Queue  nfQueue

	isRunning      atomic.Bool
	shutdownSignal = make(chan struct{})
)

// nfQueue encapsulates nfQueue providers.
type nfQueue interface {
	PacketChannel() <-chan packet.Packet
	Destroy()
}

func init() {
	v4chains = []string{
		"mangle PORTMASTER-INGEST-OUTPUT",
		"mangle PORTMASTER-INGEST-INPUT",
		"filter PORTMASTER-FILTER",
		"nat PORTMASTER-REDIRECT",
	}

	v4rules = []string{
		// stenya: Preserve original packet marks for permanently allowed connections (connmark 1710/AcceptAlways)
		// to ensure compatibility with other tools that also rely on packet marks.
		// This rule is placed before `CONNMARK --restore-mark` to prevent overwriting the original mark.
		// (Example: WireGuard/wg-quick relies on packet marks; changing them would break its routing).
		"mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
		"mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
		"mangle PORTMASTER-INGEST-OUTPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-OUTPUT -m mark --mark 0 -j NFQUEUE --queue-num 17040 --queue-bypass",

		// stenya: Preserve original packet marks, similar to the OUTPUT chain (not sure if this is really needed for INPUT).
		"mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
		"mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
		"mangle PORTMASTER-INGEST-INPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-INPUT -m mark --mark 0 -j NFQUEUE --queue-num 17140 --queue-bypass",

		"filter PORTMASTER-FILTER -m mark --mark 0 -j DROP",
		// stenya: Preserve original packet marks.
		"filter PORTMASTER-FILTER -m connmark --mark 1710 -j RETURN",
		"filter PORTMASTER-FILTER -m connmark --mark 1709 -j ACCEPT",
		"filter PORTMASTER-FILTER -m mark --mark 1700 -j RETURN",
		// Accepting ICMP packets with mark 1701 is required for rejecting to work,
		// as the rejection ICMP packet will have the same mark. Blocked ICMP
		// packets will always result in a drop within the Portmaster.
		"filter PORTMASTER-FILTER -m mark --mark 1701 -p icmp -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1701 -j REJECT --reject-with icmp-admin-prohibited",
		"filter PORTMASTER-FILTER -m mark --mark 1702 -j DROP",
		"filter PORTMASTER-FILTER -j CONNMARK --save-mark",
		"filter PORTMASTER-FILTER -m mark --mark 1710 -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1709 -j ACCEPT",
		// Accepting ICMP packets with mark 1711 is required for rejecting to work,
		// as the rejection ICMP packet will have the same mark. Blocked ICMP
		// packets will always result in a drop within the Portmaster.
		"filter PORTMASTER-FILTER -m mark --mark 1711 -p icmp -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1711 -j REJECT --reject-with icmp-admin-prohibited",
		"filter PORTMASTER-FILTER -m mark --mark 1712 -j DROP",
		"filter PORTMASTER-FILTER -m mark --mark 1717 -j RETURN",

		"nat PORTMASTER-REDIRECT -m mark --mark 1799 -p udp -j DNAT --to 127.0.0.17:53",
		"nat PORTMASTER-REDIRECT -m mark --mark 1717 -p tcp -j DNAT --to 127.0.0.17:717",
		"nat PORTMASTER-REDIRECT -m mark --mark 1717 -p udp -j DNAT --to 127.0.0.17:717",
		// "nat PORTMASTER-REDIRECT -m mark --mark 1717 ! -p tcp ! -p udp -j DNAT --to 127.0.0.17",
	}

	v4once = []string{
		"mangle OUTPUT -j PORTMASTER-INGEST-OUTPUT",
		"mangle INPUT -j PORTMASTER-INGEST-INPUT",
		"filter OUTPUT -j PORTMASTER-FILTER",
		"filter INPUT -j PORTMASTER-FILTER",
		"nat OUTPUT -j PORTMASTER-REDIRECT",
	}

	v6chains = []string{
		"mangle PORTMASTER-INGEST-OUTPUT",
		"mangle PORTMASTER-INGEST-INPUT",
		"filter PORTMASTER-FILTER",
		"nat PORTMASTER-REDIRECT",
	}

	v6rules = []string{
		"mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
		"mangle PORTMASTER-INGEST-OUTPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
		"mangle PORTMASTER-INGEST-OUTPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-OUTPUT -m mark --mark 0 -j NFQUEUE --queue-num 17060 --queue-bypass",

		"mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1710 -j RETURN",
		"mangle PORTMASTER-INGEST-INPUT -m mark ! --mark 0 -m connmark --mark 1709 -j RETURN",
		"mangle PORTMASTER-INGEST-INPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-INPUT -m mark --mark 0 -j NFQUEUE --queue-num 17160 --queue-bypass",

		"filter PORTMASTER-FILTER -m mark --mark 0 -j DROP",
		"filter PORTMASTER-FILTER -m connmark --mark 1710 -j RETURN",
		"filter PORTMASTER-FILTER -m connmark --mark 1709 -j ACCEPT",
		"filter PORTMASTER-FILTER -m mark --mark 1700 -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1701 -p icmpv6 -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1701 -j REJECT --reject-with icmp6-adm-prohibited",
		"filter PORTMASTER-FILTER -m mark --mark 1702 -j DROP",
		"filter PORTMASTER-FILTER -j CONNMARK --save-mark",
		"filter PORTMASTER-FILTER -m mark --mark 1710 -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1709 -j ACCEPT",
		"filter PORTMASTER-FILTER -m mark --mark 1711 -p icmpv6 -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1711 -j REJECT --reject-with icmp6-adm-prohibited",
		"filter PORTMASTER-FILTER -m mark --mark 1712 -j DROP",
		"filter PORTMASTER-FILTER -m mark --mark 1717 -j RETURN",

		"nat PORTMASTER-REDIRECT -m mark --mark 1799 -p udp -j DNAT --to [::1]:53",
		"nat PORTMASTER-REDIRECT -m mark --mark 1717 -p tcp -j DNAT --to [::1]:717",
		"nat PORTMASTER-REDIRECT -m mark --mark 1717 -p udp -j DNAT --to [::1]:717",
		// "nat PORTMASTER-REDIRECT -m mark --mark 1717 ! -p tcp ! -p udp -j DNAT --to [::1]",
	}

	v6once = []string{
		"mangle OUTPUT -j PORTMASTER-INGEST-OUTPUT",
		"mangle INPUT -j PORTMASTER-INGEST-INPUT",
		"filter OUTPUT -j PORTMASTER-FILTER",
		"filter INPUT -j PORTMASTER-FILTER",
		"nat OUTPUT -j PORTMASTER-REDIRECT",
	}

	// Reverse because we'd like to insert in a loop
	_ = sort.Reverse(sort.StringSlice(v4once)) // silence vet (sort is used just like in the docs)
	_ = sort.Reverse(sort.StringSlice(v6once)) // silence vet (sort is used just like in the docs)
}

func activateNfqueueFirewall() error {
	if err := activateIPTables(iptables.ProtocolIPv4, v4rules, v4once, v4chains); err != nil {
		return err
	}

	if netenv.IPv6Enabled() {
		if err := activateIPTables(iptables.ProtocolIPv6, v6rules, v6once, v6chains); err != nil {
			return err
		}
	}

	if err := nfq.InitNFCT(); err != nil {
		return err
	}
	_ = nfq.DeleteAllMarkedConnection()

	return nil
}

// DeactivateNfqueueFirewall drops portmaster related IP tables rules.
// Any errors encountered accumulated into a *multierror.Error.
func DeactivateNfqueueFirewall() error {
	// IPv4
	var result *multierror.Error
	if err := deactivateIPTables(iptables.ProtocolIPv4, v4once, v4chains); err != nil {
		result = multierror.Append(result, err)
	}

	// IPv6
	if netenv.IPv6Enabled() {
		if err := deactivateIPTables(iptables.ProtocolIPv6, v6once, v6chains); err != nil {
			result = multierror.Append(result, err)
		}
	}

	_ = nfq.DeleteAllMarkedConnection()
	nfq.TeardownNFCT()

	return result.ErrorOrNil()
}

func activateIPTables(protocol iptables.Protocol, rules, once, chains []string) error {
	tbls, err := iptables.NewWithProtocol(protocol)
	if err != nil {
		return err
	}

	for _, chain := range chains {
		splittedRule := strings.Split(chain, " ")
		if err = tbls.ClearChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
	}

	for _, rule := range rules {
		splittedRule := strings.Split(rule, " ")
		if err = tbls.Append(splittedRule[0], splittedRule[1], splittedRule[2:]...); err != nil {
			return err
		}
	}

	for _, rule := range once {
		splittedRule := strings.Split(rule, " ")

		err := tbls.InsertUnique(splittedRule[0], splittedRule[1], 1, splittedRule[2:]...)
		if err != nil {
			return err
		}
	}

	return nil
}

// ensureJumpRulesAtTop ensures that all "once" rules (the jump rules
// into Portmaster chains) are at the first position in their respective chains
// for both IPv4 and IPv6. It returns the list of rules that were out of position
// and had to be reinserted.
func ensureJumpRulesAtTop() (reinsertedRules []string, err error) {
	reinsertedRules, err = reinsertDisplacedRules(iptables.ProtocolIPv4, v4once)
	if err != nil {
		return nil, err
	}

	if netenv.IPv6Enabled() {
		v6ReinsertedRules, err := reinsertDisplacedRules(iptables.ProtocolIPv6, v6once)
		if err != nil {
			return nil, err
		}
		reinsertedRules = append(reinsertedRules, v6ReinsertedRules...)
	}

	return reinsertedRules, nil
}

// reinsertDisplacedRules checks each rule in once and, if it is not already
// at position 1 of its chain, moves it there. To avoid a window where packets
// can bypass the firewall, a temporary placeholder rule is inserted first,
// then the original rule is deleted and reinserted at position 1, and finally
// the placeholder is removed. Returns the subset of rules that required moving.
// Required rules format example: "filter OUTPUT -j PORTMASTER-FILTER"
func reinsertDisplacedRules(protocol iptables.Protocol, once []string) (reinsertedRules []string, err error) {
	tbls, err := iptables.NewWithProtocol(protocol)
	if err != nil {
		return nil, err
	}
	var rulesToUpdate []string
	for _, onceRule := range once {
		splittedRule := strings.Split(onceRule, " ")
		table := splittedRule[0]
		chain := splittedRule[1]
		// get the first rule of the chain
		firstRule, err := tbls.ListById(table, chain, 1)
		if err != nil {
			return nil, err
		}
		// check if the first rule of the chain is the portmaster rule
		pmChainName := splittedRule[len(splittedRule)-1]
		if !strings.HasSuffix(firstRule, pmChainName) {
			rulesToUpdate = append(rulesToUpdate, onceRule)
		}
	}

	comment := []string{"-m", "comment", "--comment", `TEMPORARY_RULE`}
	for _, rule := range rulesToUpdate {
		splittedRule := strings.Split(rule, " ")
		table := splittedRule[0]     // "filter"
		chain := splittedRule[1]     // "OUTPUT"
		ruleSpec := splittedRule[2:] // "-j PORTMASTER-FILTER"

		tmpRuleSpec := append(append([]string{}, comment...), ruleSpec...) // "-m comment --comment "TEMPORARY_RULE" -j PORTMASTER-FILTER"

		// Insert the temporary rule on the first position
		err = tbls.Insert(table, chain, 1, tmpRuleSpec...)
		if err != nil {
			return nil, fmt.Errorf("failed to insert temporary rule '%s' into chain '%s' in table '%s': %w", tmpRuleSpec, chain, table, err)
		}
		// delete the original rule and re-insert it on the first position
		err = tbls.Delete(table, chain, ruleSpec...)
		if err != nil {
			return nil, fmt.Errorf("failed to delete original rule '%s' from chain '%s' in table '%s': %w", ruleSpec, chain, table, err)
		}
		err = tbls.Insert(table, chain, 1, ruleSpec...)
		if err != nil {
			return nil, fmt.Errorf("failed to re-insert original rule '%s' into chain '%s' in table '%s': %w", ruleSpec, chain, table, err)
		}
		// delete the temporary rule
		err = tbls.Delete(table, chain, tmpRuleSpec...)
		if err != nil {
			return nil, fmt.Errorf("failed to delete temporary rule '%s' from chain '%s' in table '%s': %w", tmpRuleSpec, chain, table, err)
		}
	}

	return rulesToUpdate, nil
}

func deactivateIPTables(protocol iptables.Protocol, rules, chains []string) error {
	tbls, err := iptables.NewWithProtocol(protocol)
	if err != nil {
		return err
	}

	var multierr *multierror.Error

	for _, rule := range rules {
		splittedRule := strings.Split(rule, " ")
		ok, err := tbls.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
		if err != nil {
			multierr = multierror.Append(multierr, err)
		}
		if ok {
			if err = tbls.Delete(splittedRule[0], splittedRule[1], splittedRule[2:]...); err != nil {
				multierr = multierror.Append(multierr, err)
			}
		}
	}

	for _, chain := range chains {
		splittedRule := strings.Split(chain, " ")
		if err = tbls.ClearChain(splittedRule[0], splittedRule[1]); err != nil {
			multierr = multierror.Append(multierr, err)
		}
		if err = tbls.DeleteChain(splittedRule[0], splittedRule[1]); err != nil {
			multierr = multierror.Append(multierr, err)
		}
	}

	return multierr.ErrorOrNil()
}

// StartNfqueueInterception starts the nfqueue interception.
func StartNfqueueInterception(packets chan<- packet.Packet) (err error) {
	if !isRunning.CompareAndSwap(false, true) {
		return nil // already running
	}

	// Reset shutdown signal
	shutdownSignal = make(chan struct{})

	err = activateNfqueueFirewall()
	if err != nil {
		return fmt.Errorf("could not initialize nfqueue: %w", err)
	}

	out4Queue, err = nfq.New(17040, false)
	if err != nil {
		return fmt.Errorf("nfqueue(IPv4, out): %w", err)
	}
	in4Queue, err = nfq.New(17140, false)
	if err != nil {
		return fmt.Errorf("nfqueue(IPv4, in): %w", err)
	}

	if netenv.IPv6Enabled() {
		out6Queue, err = nfq.New(17060, true)
		if err != nil {
			return fmt.Errorf("nfqueue(IPv6, out): %w", err)
		}
		in6Queue, err = nfq.New(17160, true)
		if err != nil {
			return fmt.Errorf("nfqueue(IPv6, in): %w", err)
		}
	} else {
		log.Warningf("interception: no IPv6 stack detected, disabling IPv6 network integration")
		out6Queue = &disabledNfQueue{}
		in6Queue = &disabledNfQueue{}
	}

	module.mgr.Go("nfqueue packet handler", func(_ *mgr.WorkerCtx) error {
		return handleInterception(packets)
	})

	// Safety check: ensure Portmaster's iptables jump rules remain at the top of their chains.
	// During system boot, other services may insert their own iptables rules, potentially
	// displacing Portmaster's rules and causing traffic to bypass the firewall.
	// The check runs a few times after startup with increasing delays to cover this window.
	// A continuous periodic check is intentionally avoided - it would not react immediately
	// to rule changes anyway, and the overhead is not justified after the boot stage.
	// TODO: consider a more reactive approach using netlink to detect iptables changes in real time.
	module.mgr.Go("iptables rule order maintenance (startup)", func(w *mgr.WorkerCtx) error {
		for _, d := range []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second} {
			select {
			case <-time.After(d):
			case <-w.Done():
				return nil
			case <-shutdownSignal:
				return nil
			}
			if updatedRules, err := ensureJumpRulesAtTop(); err != nil {
				log.Errorf("interception: failed to ensure iptables jump rules at top: %v", err)
			} else if len(updatedRules) > 0 {
				log.Warningf("interception: the following iptables rules were found out of position and have been reinserted at the top of their chains: %v", updatedRules)
			}
		}
		return nil
	})

	return nil
}

// StopNfqueueInterception stops the nfqueue interception.
func StopNfqueueInterception() error {
	if !isRunning.CompareAndSwap(true, false) {
		return nil // not running
	}

	// Signal shutdown to packet handler
	defer close(shutdownSignal)

	if out4Queue != nil {
		out4Queue.Destroy()
	}
	if in4Queue != nil {
		in4Queue.Destroy()
	}
	if out6Queue != nil {
		out6Queue.Destroy()
	}
	if in6Queue != nil {
		in6Queue.Destroy()
	}

	err := DeactivateNfqueueFirewall()
	if err != nil {
		return fmt.Errorf("interception: error while deactivating nfqueue: %w", err)
	}

	return nil
}

func handleInterception(packets chan<- packet.Packet) error {
	for {
		var pkt packet.Packet
		select {
		case <-shutdownSignal:
			return nil
		case pkt = <-out4Queue.PacketChannel():
			pkt.SetOutbound()
		case pkt = <-in4Queue.PacketChannel():
			pkt.SetInbound()
		case pkt = <-out6Queue.PacketChannel():
			pkt.SetOutbound()
		case pkt = <-in6Queue.PacketChannel():
			pkt.SetInbound()
		}

		select {
		case packets <- pkt:
		case <-shutdownSignal:
			return nil
		}
	}
}

type disabledNfQueue struct{}

func (dnfq *disabledNfQueue) PacketChannel() <-chan packet.Packet {
	return nil
}

func (dnfq *disabledNfQueue) Destroy() {}
