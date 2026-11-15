// Connection model fixtures: the JSON form of connections, their database
// keys and IDs, and the verdict names.
package main

import (
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/resolver"
	"github.com/safing/structures/dsd"
)

type networkConnFixture struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	JSON string `json:"json"`
}

type networkIDFixture struct {
	Protocol uint8  `json:"protocol"`
	Src      string `json:"src"`
	SrcPort  uint16 `json:"src_port"`
	Dst      string `json:"dst"`
	DstPort  uint16 `json:"dst_port"`
	Inbound  bool   `json:"inbound"`
	ID       string `json:"id"`
}

type networkVerdictFixture struct {
	Value  int8   `json:"value"`
	String string `json:"string"`
	Verb   string `json:"verb"`
}

func networkKey(pid int, scope, id string) string {
	return fmt.Sprintf("network:tree/%d/%s/%s", pid, scope, id)
}

// genNetwork records connection records as the Go core serializes them.
func genNetwork() {
	modified := time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)
	expires := time.Date(2023, 11, 14, 22, 18, 20, 123456000, time.UTC)

	var conns []networkConnFixture
	add := func(name string, pid int, scope string, conn *network.Connection) {
		dumped, err := dsd.Dump(conn, dsd.JSON)
		if err != nil {
			fail(err)
		}
		conns = append(conns, networkConnFixture{
			Name: name,
			Key:  networkKey(pid, scope, conn.ID),
			JSON: string(dumped[1:]),
		})
	}

	// Outbound TCP connection with every field populated.
	add("ip_outbound_full", 4242, "ip", &network.Connection{
		ID:           "6-192.168.1.5-43210-93.184.216.34-443",
		Type:         network.IPConnection,
		Scope:        "example.com",
		IPVersion:    packet.IPv4,
		IPProtocol:   packet.TCP,
		LocalIP:      net.ParseIP("192.168.1.5"),
		LocalIPScope: netutils.SiteLocal,
		LocalPort:    43210,
		PID:          4242,
		Entity: &intel.Entity{
			IP:            net.ParseIP("93.184.216.34"),
			IPScope:       netutils.Global,
			Protocol:      6,
			Port:          443,
			Domain:        "example.com",
			ReverseDomain: "reverse.example.net.",
			CNAME:         []string{"www.example.com."},
			Country:       "US",
			Coordinates:   &geoip.Coordinates{AccuracyRadius: 100, Latitude: 37.751, Longitude: -97.822},
			ASN:           15133,
			ASOrg:         "Edgecast <Inc> & Co",
			LocationError: "",
			BlockedByLists: []string{
				"TRAC",
			},
			BlockedEntities: []string{"example.com"},
			ListOccurences: map[string][]string{
				"example.com": {"TRAC", "MAL"},
				"AS15133":     {"MAL"},
			},
			ListsError: "",
		},
		Resolver: &resolver.ResolverInfo{
			Name:    "Cloudflare",
			Type:    "dot",
			Source:  "config",
			IP:      net.ParseIP("1.1.1.1"),
			Domain:  "cloudflare-dns.com",
			IPScope: netutils.Global,
			Port:    853,
		},
		Verdict:               network.VerdictRerouteToSplitTun,
		ConnectionEstablished: true,
		Reason: network.Reason{
			Msg:       "allowed by rule",
			OptionKey: "filter/endpoints",
			Profile:   "local/abc",
			Context:   map[string]any{"b": 2, "a": "x", "list": []string{"p", "q"}},
		},
		Started:          1700000000,
		Ended:            1700000100,
		VerdictPermanent: true,
		Inspecting:       true,
		Tunneled:         true,
		Encrypted:        true,
		TunnelOpts:       map[string]any{"mode": "fast"},
		ProcessContext: network.ProcessContext{
			ProcessName: "firefox",
			ProfileName: "Firefox",
			BinaryPath:  "/usr/lib/firefox/firefox",
			CmdLine:     "firefox --new-window",
			PID:         4242,
			CreatedAt:   1700000000000,
			Profile:     "abc",
			Source:      "local",
		},
		DNSContext: &resolver.DNSRequestContext{
			Domain:          "example.com.",
			Question:        "example.com. IN A",
			RCode:           "NOERROR",
			ServedFromCache: true,
			RequestingNew:   false,
			IsBackup:        true,
			Filtered:        false,
			Modified:        modified,
			Expires:         expires,
		},
		SplitTunContext: &network.SplitTunContext{
			Interface: "wg0",
			IP:        net.ParseIP("10.8.0.2"),
			Proxy:     "socks5://proxy.example:1080",
		},
		HistoryEnabled:         true,
		BandwidthEnabled:       true,
		BytesReceived:          1 << 40,
		BytesSent:              12345,
		Internal:               true,
		ProfileRevisionCounter: 7,
	})

	// Inbound UDP IPv6 connection with minimal data.
	add("ip_inbound_minimal", -3, "ip", &network.Connection{
		ID:           "17-2001:db8::1-5353-2001:db8::2-40000",
		Type:         network.IPConnection,
		Scope:        network.IncomingInternet,
		IPVersion:    packet.IPv6,
		Inbound:      true,
		IPProtocol:   packet.UDP,
		LocalIP:      net.ParseIP("2001:db8::1"),
		LocalIPScope: netutils.Global,
		LocalPort:    5353,
		PID:          -3,
		Entity: &intel.Entity{
			IP:       net.ParseIP("2001:db8::2"),
			IPScope:  netutils.Global,
			Protocol: 17,
			Port:     40000,
		},
		Verdict: network.VerdictDrop,
		Started: 1700000200,
		ProcessContext: network.ProcessContext{
			ProcessName: "Network Noise",
			PID:         -3,
		},
		SplitTunContext: &network.SplitTunContext{
			Interface: "eth0",
			IP:        net.ParseIP("192.0.2.9"),
		},
	})

	// ICMP connection without ports and an IPv4-mapped IPv6 address.
	add("ip_icmp_mapped", -2, "ip", &network.Connection{
		ID:           "1-10.0.0.2-8.8.8.8",
		Type:         network.IPConnection,
		Scope:        "PI",
		IPVersion:    packet.IPv4,
		IPProtocol:   packet.ICMP,
		LocalIP:      net.ParseIP("::ffff:10.0.0.2"),
		LocalIPScope: netutils.SiteLocal,
		PID:          -2,
		Entity: &intel.Entity{
			IP:       net.ParseIP("8.8.8.8"),
			IPScope:  netutils.Global,
			Protocol: 1,
		},
		Verdict: network.VerdictUndecided,
		Started: 1700000300,
	})

	// DNS request connection.
	add("dns_request", 4242, "dns", &network.Connection{
		ID:       "dns-4242-1",
		Type:     network.DNSRequest,
		External: true,
		Scope:    "example.org.",
		PID:      4242,
		Entity: &intel.Entity{
			Domain:  "example.org.",
			IPScope: netutils.Global,
		},
		Verdict: network.VerdictBlock,
		Reason:  network.Reason{Msg: "blocked by list"},
		Started: 1700000400,
		Ended:   1700000400,
		ProcessContext: network.ProcessContext{
			ProcessName: "curl",
			PID:         4242,
		},
	})

	// Entity after MatchLists without a hit: the block lists are empty but
	// not nil, so they serialize as [].
	matched := &intel.Entity{
		IP:       net.ParseIP("1.1.1.1"),
		IPScope:  netutils.Global,
		Protocol: 6,
		Port:     443,
		Domain:   "one.example",
	}
	matched.MatchLists([]string{"NOPE"})
	add("ip_matched_empty_lists", 4242, "ip", &network.Connection{
		ID:         "6-192.168.1.5-50000-1.1.1.1-443",
		Type:       network.IPConnection,
		Scope:      "one.example",
		IPVersion:  packet.IPv4,
		IPProtocol: packet.TCP,
		LocalIP:    net.ParseIP("192.168.1.5"),
		LocalPort:  50000,
		PID:        4242,
		Entity:     matched,
		Started:    1700000500,
	})

	var ids []networkIDFixture
	type idCase struct {
		protocol packet.IPProtocol
		src      string
		srcPort  uint16
		dst      string
		dstPort  uint16
		inbound  bool
	}
	for _, c := range []idCase{
		{packet.TCP, "192.168.1.5", 43210, "93.184.216.34", 443, false},
		{packet.TCP, "93.184.216.34", 443, "192.168.1.5", 43210, true},
		{packet.UDP, "2001:db8::1", 5353, "ff02::fb", 5353, false},
		{packet.UDP, "::ffff:10.0.0.2", 1000, "::ffff:10.0.0.3", 2000, true},
		{packet.ICMP, "10.0.0.2", 0, "8.8.8.8", 0, false},
		{packet.ICMPv6, "fe80::1", 0, "fe80::2", 0, true},
		{packet.UDPLite, "10.0.0.2", 7, "10.0.0.3", 8, false},
		{packet.TCP, "0.0.0.0", 1, "255.255.255.255", 2, false},
	} {
		ids = append(ids, networkIDFixture{
			Protocol: uint8(c.protocol),
			Src:      c.src,
			SrcPort:  c.srcPort,
			Dst:      c.dst,
			DstPort:  c.dstPort,
			Inbound:  c.inbound,
			ID: packet.CreateConnectionID(
				c.protocol, net.ParseIP(c.src), c.srcPort, net.ParseIP(c.dst), c.dstPort, c.inbound,
			),
		})
	}

	var verdicts []networkVerdictFixture
	for v := network.Verdict(0); v <= 9; v++ {
		verdicts = append(verdicts, networkVerdictFixture{
			Value:  int8(v),
			String: v.String(),
			Verb:   v.Verb(),
		})
	}

	writeObject("network.json", map[string]any{
		"connections": conns,
		"ids":         ids,
		"verdicts":    verdicts,
	})
}
