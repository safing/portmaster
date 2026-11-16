// Fixtures for the Rust port of service/resolver: the dns/* options, the
// zone-file text of cached records and the persisted NameRecord JSON.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	_ "unsafe" // Required for go:linkname.

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/resolver"
)

// registerResolverConfiguration is the unexported registration of the
// resolver package; linking to it avoids copying the option definitions.
//
//go:linkname registerResolverConfiguration github.com/safing/portmaster/service/resolver.prepConfig
func registerResolverConfiguration() error

func genResolver() {
	if err := registerResolverConfiguration(); err != nil {
		fail(err)
	}

	var options []*config.Option
	for _, option := range config.ExportOptions() {
		if strings.HasPrefix(option.Key, "dns/") {
			options = append(options, option)
		}
	}
	writeObject("resolver_options.json", options)

	writeObject("resolver.json", map[string]any{
		"rr_texts":     resolverRRTexts(),
		"name_records": resolverNameRecords(),
		"ip_infos":     resolverIPInfos(),
	})
}

// resolverRRTexts records how Go prints (and re-reads) resource records.
func resolverRRTexts() []any {
	inputs := []string{
		"example.com. 300 IN A 192.0.2.1",
		"example.com. 17 IN AAAA 2001:db8::1",
		"example.com. 17 IN AAAA ::ffff:192.0.2.1",
		"www.example.com. 17 IN CNAME example.net.",
		"example.com. 17 IN NS ns1.example.com.",
		"1.2.0.192.in-addr.arpa. 17 IN PTR host.example.com.",
		"example.com. 17 IN MX 10 mail.example.com.",
		`example.com. 17 IN TXT "v=spf1 -all"`,
		`example.com. 17 IN TXT "a" "b c" "quote\"back\\slash"`,
		`example.com. 17 IN TXT "semi;colon" "tab\009" "nl\010" "del\127" "high\200"`,
		"example.com. 17 IN SOA ns1.example.com. hostmaster.example.com. 2024010101 7200 3600 1209600 300",
		"_sip._tcp.example.com. 17 IN SRV 10 60 5060 sip.example.com.",
		`example.com. 17 IN CAA 0 issue "letsencrypt.org"`,
		"example.com. 17 IN DNAME example.net.",
		`example.com. 17 IN HINFO "cpu" "os"`,
		"example.com. 17 IN HTTPS 1 . alpn=h2,h3",
		"example.com. 17 IN HTTPS 0 svc.example.net.",
		"example.com. 17 IN SVCB 1 svc.example.net. port=8443 ipv4hint=192.0.2.1,192.0.2.2",
		"example.com. 17 IN TYPE999 \\# 4 0a000001",
		"example.com. 17 IN HTTPS \\# 6 000100000100",
		"example.com. 17 IN NAPTR 100 50 \"s\" \"http\" \"\" _http._tcp.example.com.",
		"example.com. 17 IN SSHFP 1 1 123456789abcdef67890123456789abcdef67890",
		"example.com. 17 IN TLSA 3 1 1 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"Example.COM. 17 IN A 192.0.2.1",
		`we\.ird\032name.example.com. 17 IN A 192.0.2.1`,
		". 17 IN NS a.root-servers.net.",
		"example.com. 17 CH TXT \"chaos\"",
	}

	var cases []any
	for _, in := range inputs {
		c := map[string]any{"in": in}
		rr, err := dns.NewRR(in)
		switch {
		case err != nil:
			c["error"] = err.Error()
		case rr == nil:
			c["error"] = "nil"
		default:
			c["out"] = rr.String()
			c["type"] = dns.TypeToString[rr.Header().Rrtype]
		}
		cases = append(cases, c)
	}
	return cases
}

func resolverNameRecords() []any {
	mustRR := func(s string) dns.RR {
		rr, err := dns.NewRR(s)
		if err != nil {
			fail(err)
		}
		return rr
	}

	info := &resolver.ResolverInfo{
		Name:    "Cloudflare",
		Type:    resolver.ServerTypeDoT,
		Source:  resolver.ServerSourceConfigured,
		IP:      net.ParseIP("1.1.1.2"),
		Domain:  "cloudflare-dns.com",
		IPScope: netutils.Global,
		Port:    853,
	}
	sysInfo := &resolver.ResolverInfo{
		Type:    resolver.ServerTypeDNS,
		Source:  resolver.ServerSourceOperatingSystem,
		IP:      net.ParseIP("192.168.1.1"),
		IPScope: netutils.SiteLocal,
		Port:    53,
	}

	samples := []struct {
		name  string
		cache *resolver.RRCache
	}{
		{"a_cname_aaaa", &resolver.RRCache{
			Domain:   "example.com.",
			Question: dns.Type(dns.TypeA),
			RCode:    dns.RcodeSuccess,
			Answer: []dns.RR{
				mustRR("example.com. 17 IN CNAME www.example.net."),
				mustRR("www.example.net. 17 IN A 192.0.2.1"),
				mustRR("www.example.net. 17 IN AAAA 2001:db8::1"),
			},
			Extra:    []dns.RR{mustRR("example.com. 17 IN TXT \"extra \\\"quoted\\\"\"")},
			Expires:  1_700_000_060,
			Resolver: info,
		}},
		{"nxdomain_soa", &resolver.RRCache{
			Domain:   "nothing.example.com.",
			Question: dns.Type(dns.TypeAAAA),
			RCode:    dns.RcodeNameError,
			Ns: []dns.RR{
				mustRR("example.com. 17 IN SOA ns1.example.com. hostmaster.example.com. 2024010101 7200 3600 1209600 300"),
			},
			Expires:  1_700_000_010,
			Resolver: sysInfo,
		}},
		{"empty_sections", &resolver.RRCache{
			Domain:   "empty.example.com.",
			Question: dns.Type(dns.TypeMX),
			RCode:    dns.RcodeSuccess,
			Expires:  1_700_000_060,
			Resolver: info,
		}},
	}

	var cases []any
	for _, s := range samples {
		record := s.cache.ToNameRecord()
		data, err := json.Marshal(record)
		if err != nil {
			fail(err)
		}
		// "expires" is synthesized (Expires + 14 days): Go's PutNew resets the
		// meta, so Go itself never sets a database expiry on these records.
		cases = append(cases, map[string]any{
			"name":    s.name,
			"key":     "cache:intel/nameRecord/" + record.Domain + record.Question,
			"json":    string(data),
			"expires": record.Expires + 86400*14,
		})
	}
	return cases
}

// resolverIPInfos records the persisted IPInfo JSON and the key of a few
// records, including AddDomain results.
func resolverIPInfos() []any {
	info := &resolver.ResolverInfo{
		Name:    "Cloudflare",
		Type:    resolver.ServerTypeDoT,
		Source:  resolver.ServerSourceConfigured,
		IP:      net.ParseIP("1.1.1.2"),
		Domain:  "cloudflare-dns.com",
		IPScope: netutils.Global,
		Port:    853,
	}
	rrCache := &resolver.RRCache{
		Domain:          "example.com.",
		Question:        dns.Type(dns.TypeA),
		RCode:           dns.RcodeSuccess,
		Expires:         1_700_000_060,
		Modified:        1_700_000_000,
		ServedFromCache: true,
		Resolver:        info,
	}
	plain := &resolver.RRCache{
		Domain:   "plain.example.net.",
		Question: dns.Type(dns.TypeAAAA),
		RCode:    dns.RcodeNameError,
		Expires:  1_700_000_100,
	}

	one := &resolver.IPInfo{IP: "192.0.2.1", ProfileID: "global"}
	one.AddDomain(resolver.ResolvedDomain{
		Domain:            "example.com.",
		Resolver:          info,
		DNSRequestContext: rrCache.ToDNSRequestContext(),
		Expires:           rrCache.Expires,
	})

	many := &resolver.IPInfo{IP: "2001:db8::1", ProfileID: "local/abc"}
	many.AddDomain(resolver.ResolvedDomain{
		Domain:  "sub1.example.com.",
		CNAMEs:  []string{"example.com."},
		Expires: 1_700_000_050,
	})
	many.AddDomain(resolver.ResolvedDomain{
		Domain:            "plain.example.net.",
		DNSRequestContext: plain.ToDNSRequestContext(),
		Expires:           plain.Expires,
	})
	many.AddDomain(resolver.ResolvedDomain{
		Domain: "sub1.example.com.",
		CNAMEs: []string{},
	})

	var cases []any
	for _, s := range []*resolver.IPInfo{one, many} {
		data, err := json.Marshal(s)
		if err != nil {
			fail(err)
		}
		cases = append(cases, map[string]any{
			"key":  "cache:intel/ipInfo/" + s.ProfileID + "/" + s.IP,
			"json": string(data),
		})
	}
	return cases
}

// verifyNameRecord reads a NameRecord JSON written by the Rust port the way the
// Go resolver does and prints the re-serialized form plus every parsed RR.
func verifyNameRecord(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	var record resolver.NameRecord
	if err := json.Unmarshal(data, &record); err != nil {
		fail(err)
	}
	if !record.IsValid() {
		fail(fmt.Errorf("record is invalid"))
	}
	out := map[string]any{"domain": record.Domain, "question": record.Question}
	var rrs []string
	for _, section := range [][]string{record.Answer, record.Ns, record.Extra} {
		for _, entry := range section {
			rr, err := dns.NewRR(entry)
			if err != nil {
				fail(fmt.Errorf("go cannot parse %q: %w", entry, err))
			}
			rrs = append(rrs, rr.String())
		}
	}
	out["rrs"] = rrs
	again, err := json.Marshal(&record)
	if err != nil {
		fail(err)
	}
	out["roundtrip"] = string(again)
	enc, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(enc))
}
