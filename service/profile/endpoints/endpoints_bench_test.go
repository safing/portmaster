package endpoints

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/safing/portmaster/service/intel"
)

const (
	ruleExact   = "- example.com"
	ruleZone    = "+ .example.com"
	ruleZonePPP = "- .example.com TCP/443"
	rulePrefix  = "- stats.*"
	wwwDomain   = "www.example.com."
	apexDomain  = "example.com."
)

// benchRules is a realistic mix of endpoint rules as found in a user's
// profile: mostly domain rules of all match types, followed by a few IP,
// network, country, ASN, scope and filter list rules. Country and ASN rules
// are allow rules, so that missing geoip data in tests does not end matching.
var benchRules = []string{
	"- ads.example.net",
	"- tracker.example.org",
	"- .doubleclick.net",
	"- .googlesyndication.com",
	"- .facebook.com",
	"- .fbcdn.net",
	"- *.telemetry.microsoft.com",
	"- *analytics*",
	"- *metrics*",
	rulePrefix,
	"- *.hotjar.com",
	"+ .github.com",
	"+ .githubusercontent.com",
	"+ api.mozilla.org",
	"+ *.debian.org",
	"+ .ubuntu.com TCP/443",
	"+ updates.example.com UDP/443",
	"- *.ru",
	"- .cn",
	"+ .wikipedia.org",
	"+ .wikimedia.org",
	"- 203.0.113.7",
	"- 198.51.100.0/24",
	"- 2001:db8::/32",
	"+ 192.168.0.0/16",
	"+ AS64496",
	"+ AS64511",
	"+ CH",
	"+ AT",
	"+ Localhost",
	"- L:TRAC,MAL",
	"+ LAN",
	"- * TCP/23",
	"- * UDP/137-139",
}

func newBenchEntity(ctx context.Context) *intel.Entity {
	entity := (&intel.Entity{
		Domain:   "www.service.example.com.",
		CNAME:    []string{"www.service.example.com.edgekey.net.", "e1234.a.akamaiedge.net.", "lb.cdn.example.net."},
		IP:       net.ParseIP("93.184.216.34"),
		Protocol: 6,
		Port:     443,
		// Pre-loaded list data, as it would be after LoadLists.
		ListOccurences: map[string][]string{
			"lb.cdn.example.net.": {"ADS"},
		},
	}).Init(0)
	entity.EnableCNAMECheck(ctx, true)
	return entity
}

// BenchmarkEndpointsMatch measures matching a connection entity that matches
// no rule against a realistic rule list, which is the most common case: every
// rule has to be evaluated against the domain and all its CNAMEs.
func BenchmarkEndpointsMatch(b *testing.B) {
	eps, err := ParseEndpoints(benchRules)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	entity := newBenchEntity(ctx)
	// Fetch (or fail to fetch) the geoip data once, as it is cached in the
	// entity and may wait for the geoip database to become available.
	entity.FetchLocation(ctx)

	b.ReportAllocs()
	for b.Loop() {
		if result, _ := eps.Match(ctx, entity); result != NoMatch {
			b.Fatalf("unexpected result %s", result)
		}
	}
}

func TestEndpointDomainCheck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tests := []struct {
		rule   string
		domain string
		cnames []string
		proto  uint8
		port   uint16
		want   EPResult
	}{
		{rule: ruleExact, domain: apexDomain, want: Denied},
		{rule: ruleExact, domain: wwwDomain, want: NoMatch},
		{rule: ruleExact, domain: "example.com", want: NoMatch}, // Missing trailing dot.
		{rule: ruleExact, domain: "", want: NoMatch},
		{rule: ruleZone, domain: apexDomain, want: Permitted},
		{rule: ruleZone, domain: "a.b.example.com.", want: Permitted},
		{rule: ruleZone, domain: "badexample.com.", want: NoMatch},
		{rule: "- *example.com", domain: "badexample.com.", want: Denied},
		{rule: "- *.example.com", domain: apexDomain, want: NoMatch},
		{rule: rulePrefix, domain: "stats.example.com.", want: Denied},
		{rule: rulePrefix, domain: "www.stats.example.com.", want: NoMatch},
		{rule: "- *track*", domain: "xn--trck-loa.tracker.example.", want: Denied},
		{rule: "- *track*", domain: "träck.example.", want: NoMatch},
		{rule: ruleZonePPP, domain: wwwDomain, proto: 6, port: 443, want: Denied},
		{rule: ruleZonePPP, domain: wwwDomain, proto: 17, port: 443, want: NoMatch},
		{rule: ruleZonePPP, domain: wwwDomain, proto: 6, port: 80, want: NoMatch},
		// CNAMEs only count for deny rules.
		{rule: "- .cdn.example", domain: wwwDomain, cnames: []string{"a.cdn.example."}, want: Denied},
		{rule: "+ .cdn.example", domain: wwwDomain, cnames: []string{"a.cdn.example."}, want: NoMatch},
		{rule: "- .cdn.example", domain: wwwDomain, cnames: []string{"", "cdn.example."}, want: Denied},
	}
	for _, tc := range tests {
		ep, err := parseEndpoint(tc.rule)
		if err != nil {
			t.Fatalf("failed to parse %q: %s", tc.rule, err)
		}
		entity := (&intel.Entity{
			Domain:   tc.domain,
			CNAME:    tc.cnames,
			Protocol: tc.proto,
			Port:     tc.port,
		}).Init(0)
		entity.EnableCNAMECheck(ctx, true)

		result, r := ep.Matches(ctx, entity)
		if result != tc.want {
			t.Errorf("%q on %q %v: got %s, want %s", tc.rule, tc.domain, tc.cnames, result, tc.want)
		}
		if (r != nil) != (result != NoMatch) {
			t.Errorf("%q on %q %v: got reason %v for result %s", tc.rule, tc.domain, tc.cnames, r, result)
		}
		if r == nil {
			continue
		}
		rc, ok := r.Context().(*reason)
		if !ok || rc.Value != strings.Fields(tc.rule)[1] {
			t.Errorf("%q on %q: unexpected reason %+v", tc.rule, tc.domain, r.Context())
		}
	}
}
