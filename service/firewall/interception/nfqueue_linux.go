package interception

import (
	"flag"
	"fmt"
	"sort"
	"strings"

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

	shutdownSignal = make(chan struct{})

	experimentalNfqueueBackend bool
)

func init() {
	flag.BoolVar(&experimentalNfqueueBackend, "experimental-nfqueue", false, "(deprecated flag; always used)")
}

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
		"mangle PORTMASTER-INGEST-OUTPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-OUTPUT -m mark --mark 0 -j NFQUEUE --queue-num 17040 --queue-bypass",

		"mangle PORTMASTER-INGEST-INPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-INPUT -m mark --mark 0 -j NFQUEUE --queue-num 17140 --queue-bypass",

		"filter PORTMASTER-FILTER -m mark --mark 0 -j DROP",
		"filter PORTMASTER-FILTER -m mark --mark 1700 -j RETURN",
		// Accepting ICMP packets with mark 1701 is required for rejecting to work,
		// as the rejection ICMP packet will have the same mark. Blocked ICMP
		// packets will always result in a drop within the Portmaster.
		"filter PORTMASTER-FILTER -m mark --mark 1701 -p icmp -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1701 -j REJECT --reject-with icmp-admin-prohibited",
		"filter PORTMASTER-FILTER -m mark --mark 1702 -j DROP",
		"filter PORTMASTER-FILTER -j CONNMARK --save-mark",
		"filter PORTMASTER-FILTER -m mark --mark 1710 -j RETURN",
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
		"mangle PORTMASTER-INGEST-OUTPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-OUTPUT -m mark --mark 0 -j NFQUEUE --queue-num 17060 --queue-bypass",

		"mangle PORTMASTER-INGEST-INPUT -j CONNMARK --restore-mark",
		"mangle PORTMASTER-INGEST-INPUT -m mark --mark 0 -j NFQUEUE --queue-num 17160 --queue-bypass",

		"filter PORTMASTER-FILTER -m mark --mark 0 -j DROP",
		"filter PORTMASTER-FILTER -m mark --mark 1700 -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1701 -p icmpv6 -j RETURN",
		"filter PORTMASTER-FILTER -m mark --mark 1701 -j REJECT --reject-with icmp6-adm-prohibited",
		"filter PORTMASTER-FILTER -m mark --mark 1702 -j DROP",
		"filter PORTMASTER-FILTER -j CONNMARK --save-mark",
		"filter PORTMASTER-FILTER -m mark --mark 1710 -j RETURN",
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
		ok, err := tbls.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
		if err != nil {
			return err
		}
		if !ok {
			if err = tbls.Insert(splittedRule[0], splittedRule[1], 1, splittedRule[2:]...); err != nil {
				return err
			}
		}
	}

	return nil
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
	// @deprecated, remove in v1
	if experimentalNfqueueBackend {
		log.Warningf("[DEPRECATED] --experimental-nfqueue has been deprecated as the backend is now used by default")
		log.Warningf("[DEPRECATED] please remove the flag from your configuration!")
	}

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
	return nil
}

// StopNfqueueInterception stops the nfqueue interception.
func StopNfqueueInterception() error {
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
