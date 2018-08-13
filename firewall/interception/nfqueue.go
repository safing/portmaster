// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

// +build linux

package interception

import (
	"sort"
	"strings"

	"github.com/coreos/go-iptables/iptables"

	"github.com/Safing/safing-core/firewall/interception/nfqueue"
	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/modules"
)

// iptables -A OUTPUT -p icmp -j", "NFQUEUE", "--queue-num", "1", "--queue-bypass

var nfqueueModule *modules.Module

var v4chains []string
var v4rules []string
var v4once []string

var v6chains []string
var v6rules []string
var v6once []string

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
	sort.Reverse(sort.StringSlice(v4once))
	sort.Reverse(sort.StringSlice(v6once))

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

	for _, rule := range v4once {
		splittedRule := strings.Split(rule, " ")
		ok, err := ip4tables.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
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

	for _, rule := range v4once {
		splittedRule := strings.Split(rule, " ")
		ok, err := ip4tables.Exists(splittedRule[0], splittedRule[1], splittedRule[2:]...)
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
		if err := ip4tables.ClearChain(splittedRule[0], splittedRule[1]); err != nil {
			return err
		}
		if err := ip4tables.DeleteChain(splittedRule[0], splittedRule[1]); err != nil {
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

func Start() {

	nfqueueModule = modules.Register("Firewall:Interception:Nfqueue", 192)

	if err := activateNfqueueFirewall(); err != nil {
		log.Criticalf("could not activate firewall for nfqueue: %q", err)
	}

	out4Queue := nfqueue.NewNFQueue(17040)
	in4Queue := nfqueue.NewNFQueue(17140)
	out6Queue := nfqueue.NewNFQueue(17060)
	in6Queue := nfqueue.NewNFQueue(17160)

	out4Channel := out4Queue.Process()
	// if err != nil {
	// log.Criticalf("could not open nfqueue out4")
	// } else {
	defer out4Queue.Destroy()
	// }
	in4Channel := in4Queue.Process()
	// if err != nil {
	// log.Criticalf("could not open nfqueue in4")
	// } else {
	defer in4Queue.Destroy()
	// }
	out6Channel := out6Queue.Process()
	// if err != nil {
	// log.Criticalf("could not open nfqueue out6")
	// } else {
	defer out6Queue.Destroy()
	// }
	in6Channel := in6Queue.Process()
	// if err != nil {
	// log.Criticalf("could not open nfqueue in6")
	// } else {
	defer in6Queue.Destroy()
	// }

packetInterceptionLoop:
	for {
		select {
		case <-nfqueueModule.Stop:
			break packetInterceptionLoop
		case pkt := <-out4Channel:
			pkt.SetOutbound()
			Packets <- pkt
		case pkt := <-in4Channel:
			pkt.SetInbound()
			Packets <- pkt
		case pkt := <-out6Channel:
			pkt.SetOutbound()
			Packets <- pkt
		case pkt := <-in6Channel:
			pkt.SetInbound()
			Packets <- pkt
		}
	}

	if err := deactivateNfqueueFirewall(); err != nil {
		log.Criticalf("could not deactivate firewall for nfqueue: %q", err)
	}

	nfqueueModule.StopComplete()

}

func stringInSlice(slice []string, value string) bool {
	for _, entry := range slice {
		if value == entry {
			return true
		}
	}
	return false
}
