package main

import (
	"context"
	"encoding/json"
	"net"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/profile/endpoints"
)

type endpointEntity struct {
	Name          string   `json:"name"`
	IP            string   `json:"ip,omitempty"`
	Domain        string   `json:"domain,omitempty"`
	CNAME         []string `json:"cname,omitempty"`
	CNAMECheck    bool     `json:"cname_check,omitempty"`
	Protocol      uint8    `json:"protocol"`
	Port          uint16   `json:"port"`
	LocationError string   `json:"location_error,omitempty"`
	// Lists is set directly as ListOccurences; Go MatchLists only reads it.
	Lists map[string][]string `json:"lists,omitempty"`
}

// Country/continent/ASN rules are not evaluated for global IPs: Go would query
// the geoip module, which is not running here. Those matches are covered by
// Rust tests against a static location provider.
var endpointEntities = []endpointEntity{
	{Name: "domain_only", Domain: "example.com."},
	{Name: "domain_tcp443", Domain: "example.com.", IP: "10.2.3.4", Protocol: 6, Port: 443},
	{Name: "sub_domain", Domain: "www.example.com.", IP: "10.2.3.4", Protocol: 6, Port: 443},
	{Name: "deep_sub", Domain: "a.b.example.com.", Protocol: 17, Port: 53},
	{Name: "other_domain", Domain: "notexample.com.", Protocol: 6, Port: 80},
	{Name: "bad_contains", Domain: "xbadx.org.", Protocol: 6, Port: 8080},
	{Name: "prefix_domain", Domain: "bad.net.", Protocol: 6, Port: 22},
	{Name: "cname_blocked", Domain: "cdn.example.org.", CNAME: []string{"tracker.bad.com.", "edge.example.net."}, CNAMECheck: true, Protocol: 6, Port: 443},
	{Name: "cname_unchecked", Domain: "cdn.example.org.", CNAME: []string{"tracker.bad.com."}, Protocol: 6, Port: 443},
	{Name: "localhost_v4", IP: "127.0.0.1", Protocol: 6, Port: 817},
	{Name: "localhost_v6", IP: "::1", Protocol: 17, Port: 53},
	{Name: "lan", IP: "192.168.0.1", Protocol: 17, Port: 1234},
	{Name: "lan_range", IP: "192.168.0.200", Protocol: 6, Port: 445},
	{Name: "link_local", IP: "fe80::1", Protocol: 58, Port: 0},
	{Name: "multicast", IP: "224.0.0.251", Protocol: 17, Port: 5353},
	{Name: "global_v4", IP: "104.82.234.109", Protocol: 6, Port: 443, LocationError: "geoip data not available (test)"},
	{Name: "global_v4_udp", IP: "104.82.234.109", Protocol: 17, Port: 27015, LocationError: "geoip data not available (test)"},
	{Name: "global_v6", IP: "2606:4700:4700::1111", Protocol: 17, Port: 53, LocationError: "geoip data not available (test)"},
	{Name: "global_icmp", IP: "1.1.1.1", Protocol: 1, Port: 0, LocationError: "geoip data not available (test)"},
	{Name: "port_1024", IP: "10.0.0.1", Protocol: 6, Port: 1024},
	{Name: "port_1025", IP: "10.0.0.1", Protocol: 6, Port: 1025},
	{Name: "no_ip_no_domain", Protocol: 6, Port: 80},
	{Name: "listed_domain", Domain: "tracker.bad.com.", Protocol: 6, Port: 443, Lists: map[string][]string{"tracker.bad.com.": {"TRAC", "ADS"}}},
	{Name: "listed_multi", Domain: "cdn.example.org.", IP: "10.9.9.9", Protocol: 17, Port: 443, Lists: map[string][]string{"10.9.9.9": {"TRAC", "MAL"}}},
	{Name: "listed_other", Domain: "ads.example.net.", Protocol: 6, Port: 80, Lists: map[string][]string{"ads.example.net.": {"ADS"}}},
}

var endpointRules = []string{
	// Any.
	"- *", "+ *", "+ * # comment", "+ *    TCP/443",
	// Domains.
	"- *bad*", "- bad*", "- *bad.com", "- .bad.com", "- bad.com", "- www.bad.com.", "- www.bad.com",
	"+ *example.com", "+ example.com", "+ .example.com", "+ example.*", "+ *.example.com", "+ www.example.com",
	"- EXAMPLE.COM", "- *bad.com TCP/443", "- *bad.com UDP",
	"+ .", "+ .at", "+ .at.", "+ 1.at", "+ 1.at.", "+ 1.f.ix.de.", "+ *contains*", "+ *has.suffix",
	"+ *.has.suffix", "+ *has.prefix*", "+ *has.prefix.*", "+ .sub.and.prefix.*", "+ *.sub..and.prefix.*",
	"+ 10.0.*", "+ a..b.com", "+ -a.com", "+ _a.com", "+ xn--a.com", "+ local.", "+ my_printer.local.",
	"+ ünï.com", "+ exa mple.com",
	// IPs and ranges.
	"+ 127.0.0.1", "+ 192.168.0.1", "+ ::1", "+ 2606:4700:4700::1111", "- 10.0.0.1 TCP/1-1024",
	"+ 127.0.0.0/8", "+ 192.168.0.0/24", "+ 2606:4700:4700::/48", "- 192.168.0.0/16 TCP/SMB", "+ 10.0.0.0/33",
	// Country, continent, ASN.
	"+ DE", "+ AT", "+ CH", "+ AS", "- DE", "+ C:EU", "- C:EU", "+ C:eu", "+ de",
	"+ AS1", "+ AS12", "+ AS123", "+ AS1234", "+ AS12345", "+ AS32590", "- AS32590",
	"+ AS32590 UDP/27015-27250", "- AS32590 UDP/27015-27250", "- DE TCP/80", "+ AS99999999999999999999",
	// Scopes.
	"+ Localhost", "+ LAN", "+ Internet", "+ Localhost,LAN,Internet", "- lan,internet", "+ LAN,LAN", "+ LAN,foo",
	// Lists.
	"- L:TRAC,MAL", "+ L:", "- L:TRAC TCP/443", "+ L:ADS", "- L:MAL UDP",
	// Protocols and ports.
	"+ * TCP/1-1024", "+ * */DNS", "+ * ICMP", "+ * 127", "+ * UDP/1234", "+ * TCP/HTTP", "+ * TCP/80-443",
	"+ * udp/dns", "+ * TCP/dhcp-server", "+ * TCP/443-80", "+ * 6/443", "+ * ICMP6", "+ * UDP-LITE",
	// Invalid.
	"", "+", "*", "x *", "+ * TCP/", "+ * TCP/*", "+ * /80", "+ * */*", "+ * TCP/0", "+ * TCP/1-0",
	"+ * TCP/1-2-3", "+ * TCP/80/90", "+ * FOO", "+ * 300", "+ * TCP/70000", "+ * TCP/80 extra",
	"+ 1234", "+ bad..com",
	// Exotic forms.
	"+ ::ffff:10.0.0.0/104", "+ ::ffff:10.0.0.0/8", "+ 10.0.0.0/0008", "+ İNTERNET", "+ * TCP/ßh", "+ AS18446744073709551615",
	"+ EXAMPLE.ORG", "+ ÉXAMPLE.ORG",
}

func newEndpointEntity(spec endpointEntity) *intel.Entity {
	e := &intel.Entity{
		Domain:         spec.Domain,
		CNAME:          spec.CNAME,
		Protocol:       spec.Protocol,
		Port:           spec.Port,
		LocationError:  spec.LocationError,
		ListOccurences: spec.Lists,
	}
	if spec.IP != "" {
		e.IP = net.ParseIP(spec.IP)
	}
	e.Init(0)
	e.EnableCNAMECheck(context.Background(), spec.CNAMECheck)
	return e
}

// needsIntelModules reports whether matching would query the geoip or filter
// list modules, which are not running in the generator.
func needsIntelModules(ep endpoints.Endpoint, spec endpointEntity) bool {
	switch ep.(type) {
	case *endpoints.EndpointCountry, *endpoints.EndpointContinent, *endpoints.EndpointASN:
		ip := net.ParseIP(spec.IP)
		return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate()
	}
	return false
}

func genEndpoints() {
	ctx := context.Background()
	var cases []any
	for _, rule := range endpointRules {
		c := map[string]any{"rule": rule}
		eps, err := endpoints.ParseEndpoints([]string{rule})
		if err != nil {
			c["error"] = err.Error()
			cases = append(cases, c)
			continue
		}
		ep := eps[0]
		c["string"] = ep.String()
		results := map[string]any{}
		for _, spec := range endpointEntities {
			if needsIntelModules(ep, spec) {
				continue
			}
			result, reason := ep.Matches(ctx, newEndpointEntity(spec))
			r := map[string]any{"result": uint8(result)}
			if reason != nil {
				r["reason"] = reason.String()
				ctxJSON, err := json.Marshal(reason.Context())
				if err != nil {
					fail(err)
				}
				r["context"] = string(ctxJSON)
			}
			results[spec.Name] = r
		}
		c["results"] = results
		cases = append(cases, c)
	}

	// Lists of rules: first decision wins, errors are counted.
	lists := [][]string{
		{"+ AS32590", "- *"},
		{"- .bad.com", "+ *"},
		{"+ example.com", "- *example.com", "- *"},
		{"bogus", "+ *", "also bogus"},
	}
	var listCases []any
	for _, list := range lists {
		c := map[string]any{"rules": list}
		eps, err := endpoints.ParseEndpoints(list)
		if err != nil {
			c["error"] = err.Error()
		}
		c["parsed"] = len(eps)
		c["string"] = eps.String()
		results := map[string]uint8{}
		for _, spec := range endpointEntities {
			if spec.LocationError != "" && list[0] == "+ AS32590" {
				// Skipped allow rule falls through to "- *" (Go test TestEndpointMatchingWithoutGeoIPData).
				continue
			}
			result, _ := eps.Match(ctx, newEndpointEntity(spec))
			results[spec.Name] = uint8(result)
		}
		c["results"] = results
		listCases = append(listCases, c)
	}

	writeObject("endpoints.json", map[string]any{
		"entities":        endpointEntities,
		"cases":           cases,
		"lists":           listCases,
		"validationRegex": endpoints.ListEntryValidationRegex,
	})
}
