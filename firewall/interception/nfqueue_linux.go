package interception

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coreos/go-iptables/iptables"

	"github.com/safing/portmaster/firewall/interception/nfqueue"
)

// iptables -A OUTPUT -p icmp -j", "NFQUEUE", "--queue-num", "1", "--queue-bypass

var (
	v4chains []string
	v4rules  []string
	v4once   []string

	v6chains []string
	v6rules  []string
	v6once   []string

	out4Queue *nfqueue.NFQueue
	in4Queue  *nfqueue.NFQueue
	out6Queue *nfqueue.NFQueue
	in6Queue  *nfqueue.NFQueue

	shutdownSignal = make(chan struct{})
)

func init() {

	v4chains = []string{
		"mangle C170",
		"mangle C171",
		"filter C17",
	}

	v4rules = []string{
		"mangle C170 -j CONNMARK --restore-mark",
		"mangle C170 -m mark --mark 0 -j NFQUEUE --queue-num 17040 --queue-bypass",

		"mangle C171 -j CONNMARK --restore-mark",
		"mangle C171 -m mark --mark 0 -j NFQUEUE --queue-num 17140 --queue-bypass",

		"filter C17 -m mark --mark 0 -j DROP",
		"filter C17 -m mark --mark 1700 -j ACCEPT",
		"filter C17 -m mark --mark 1701 -j REJECT --reject-with icmp-host-prohibited",
		"filter C17 -m mark --mark 1702 -j DROP",
		"filter C17 -j CONNMARK --save-mark",
		"filter C17 -m mark --mark 1710 -j ACCEPT",
		"filter C17 -m mark --mark 1711 -j REJECT --reject-with icmp-host-prohibited",
		"filter C17 -m mark --mark 1712 -j DROP",
		"filter C17 -m mark --mark 1717 -j ACCEPT",
	}

	v4once = []string{
		"mangle OUTPUT -j C170",
		"mangle INPUT -j C171",
		"filter OUTPUT -j C17",
		"filter INPUT -j C17",
		"nat OUTPUT -m mark --mark 1799 -p udp -j DNAT --to 127.0.0.1:53",
		"nat OUTPUT -m mark --mark 1717 -p tcp -j DNAT --to 127.0.0.17:1117",
		"nat OUTPUT -m mark --mark 1717 -p udp -j DNAT --to 127.0.0.17:1117",
		// "nat OUTPUT -m mark --mark 1717 ! -p tcp ! -p udp -j DNAT --to 127.0.0.17",
	}

	v6chains = []string{
		"mangle C170",
		"mangle C171",
		"filter C17",
	}

	v6rules = []string{
		"mangle C170 -j CONNMARK --restore-mark",
		"mangle C170 -m mark --mark 0 -j NFQUEUE --queue-num 17060 --queue-bypass",

		"mangle C171 -j CONNMARK --restore-mark",
		"mangle C171 -m mark --mark 0 -j NFQUEUE --queue-num 17160 --queue-bypass",

		"filter C17 -m mark --mark 0 -j DROP",
		"filter C17 -m mark --mark 1700 -j ACCEPT",
		"filter C17 -m mark --mark 1701 -j REJECT --reject-with icmp6-adm-prohibited",
		"filter C17 -m mark --mark 1702 -j DROP",
		"filter C17 -j CONNMARK --save-mark",
		"filter C17 -m mark --mark 1710 -j ACCEPT",
		"filter C17 -m mark --mark 1711 -j REJECT --reject-with icmp6-adm-prohibited",
		"filter C17 -m mark --mark 1712 -j DROP",
		"filter C17 -m mark --mark 1717 -j ACCEPT",
	}

	v6once = []string{
		"mangle OUTPUT -j C170",
		"mangle INPUT -j C171",
		"filter OUTPUT -j C17",
		"filter INPUT -j C17",
		"nat OUTPUT -m mark --mark 1799 -p udp -j DNAT --to [::1]:53",
		"nat OUTPUT -m mark --mark 1717 -p tcp -j DNAT --to [fd17::17]:1117",
		"nat OUTPUT -m mark --mark 1717 -p udp -j DNAT --to [fd17::17]:1117",
		// "nat OUTPUT -m mark --mark 1717 ! -p tcp ! -p udp -j DNAT --to [fd17::17]",
	}

	// Reverse because we'd like to insert in a loop
	_ = sort.Reverse(sort.StringSlice(v4once)) // silence vet (sort is used just like in the docs)
	_ = sort.Reverse(sort.StringSlice(v6once)) // silence vet (sort is used just like in the docs)

}

func activateNfqueueFirewall() error {

	// IPv4
	ip4tables, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	for _, chain := range v4chains {
		splittedRule := strings.Split(chain, " ")
		if err = ip4tables.ClearChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
	}

	for _, rule := range v4rules {
		splittedRule := strings.Split(rule, " ")
		if err = ip4tables.Append(splittedRule[0], splittedRule[1], splittedRule[2:]...); err != nil {
			return err
		}
	}

	var ok bool
	for _, rule := range v4once {
		splittedRule := strings.Split(rule, " ")
		ok, err = ip4tables.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
		if err != nil {
			return err
		}
		if !ok {
			if err = ip4tables.Insert(splittedRule[0], splittedRule[1], 1, splittedRule[2:]...); err != nil {
				return err
			}
		}
	}

	// IPv6
	ip6tables, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	if err != nil {
		return err
	}

	for _, chain := range v6chains {
		splittedRule := strings.Split(chain, " ")
		if err = ip6tables.ClearChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
	}

	for _, rule := range v6rules {
		splittedRule := strings.Split(rule, " ")
		if err = ip6tables.Append(splittedRule[0], splittedRule[1], splittedRule[2:]...); err != nil {
			return err
		}
	}

	for _, rule := range v6once {
		splittedRule := strings.Split(rule, " ")
		ok, err := ip6tables.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
		if err != nil {
			return err
		}
		if !ok {
			if err = ip6tables.Insert(splittedRule[0], splittedRule[1], 1, splittedRule[2:]...); err != nil {
				return err
			}
		}
	}

	return nil
}

func deactivateNfqueueFirewall() error {
	// IPv4
	ip4tables, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	var ok bool
	for _, rule := range v4once {
		splittedRule := strings.Split(rule, " ")
		ok, err = ip4tables.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
		if err != nil {
			return err
		}
		if ok {
			if err = ip4tables.Delete(splittedRule[0], splittedRule[1], splittedRule[2:]...); err != nil {
				return err
			}
		}
	}

	for _, chain := range v4chains {
		splittedRule := strings.Split(chain, " ")
		if err = ip4tables.ClearChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
		if err = ip4tables.DeleteChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
	}

	// IPv6
	ip6tables, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	if err != nil {
		return err
	}

	for _, rule := range v6once {
		splittedRule := strings.Split(rule, " ")
		ok, err := ip6tables.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
		if err != nil {
			return err
		}
		if ok {
			if err = ip6tables.Delete(splittedRule[0], splittedRule[1], splittedRule[2:]...); err != nil {
				return err
			}
		}
	}

	for _, chain := range v6chains {
		splittedRule := strings.Split(chain, " ")
		if err := ip6tables.ClearChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
		if err := ip6tables.DeleteChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
	}

	return nil
}

// StartNfqueueInterception starts the nfqueue interception.
func StartNfqueueInterception() (err error) {

	err = activateNfqueueFirewall()
	if err != nil {
		_ = Stop()
		return fmt.Errorf("could not initialize nfqueue: %s", err)
	}

	out4Queue, err = nfqueue.NewNFQueue(17040)
	if err != nil {
		_ = Stop()
		return fmt.Errorf("interception: failed to create nfqueue(IPv4, in): %s", err)
	}
	in4Queue, err = nfqueue.NewNFQueue(17140)
	if err != nil {
		_ = Stop()
		return fmt.Errorf("interception: failed to create nfqueue(IPv4, in): %s", err)
	}
	out6Queue, err = nfqueue.NewNFQueue(17060)
	if err != nil {
		_ = Stop()
		return fmt.Errorf("interception: failed to create nfqueue(IPv4, in): %s", err)
	}
	in6Queue, err = nfqueue.NewNFQueue(17160)
	if err != nil {
		_ = Stop()
		return fmt.Errorf("interception: failed to create nfqueue(IPv4, in): %s", err)
	}

	go handleInterception()
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

	err := deactivateNfqueueFirewall()
	if err != nil {
		return fmt.Errorf("interception: error while deactivating nfqueue: %s", err)
	}

	return nil
}

func handleInterception() {
	for {
		select {
		case <-shutdownSignal:
			return
		case pkt := <-out4Queue.Packets:
			pkt.SetOutbound()
			Packets <- pkt
		case pkt := <-in4Queue.Packets:
			pkt.SetInbound()
			Packets <- pkt
		case pkt := <-out6Queue.Packets:
			pkt.SetOutbound()
			Packets <- pkt
		case pkt := <-in6Queue.Packets:
			pkt.SetInbound()
			Packets <- pkt
		}
	}
}
