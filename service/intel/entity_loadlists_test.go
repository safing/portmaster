package intel

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"
	_ "unsafe" // For go:linkname.

	"golang.org/x/net/publicsuffix"

	"github.com/safing/portmaster/service/network/netutils"
)

// filterListsLoaded is the channel of the filterlists package that is closed
// when the filter lists are loaded. Lookups before that only log a warning.
//
//go:linkname filterListsLoaded github.com/safing/portmaster/service/intel/filterlists.filterListsLoaded
var filterListsLoaded chan struct{}

// BenchmarkLoadLists measures looking up all filter lists of the entity of a
// new connection with a domain and a CNAME, with loaded filter lists that do
// not contain the entity, like almost all entities.
func BenchmarkLoadLists(b *testing.B) {
	prevLoaded := filterListsLoaded
	filterListsLoaded = make(chan struct{})
	close(filterListsLoaded)
	b.Cleanup(func() { filterListsLoaded = prevLoaded })

	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		e := (&Entity{
			IP:       net.IPv4(93, 184, 216, 34),
			IPScope:  netutils.Global,
			Protocol: 6,
			Port:     443,
			Domain:   testDomain,
			CNAME:    []string{testCNAME},
			Country:  "US",
			ASN:      15133,
			ASOrg:    "Edgecast Inc.",
		}).Init(443)
		// The location was already looked up when the entity was created.
		e.fetchLocationOnce.Do(func() {})
		e.ResolveSubDomainLists(ctx, true)
		e.EnableCNAMECheck(ctx, true)

		e.LoadLists(ctx)
		if e.ListOccurences != nil || e.ListsError != "" {
			b.Fatalf("unexpected lists: %v %s", e.ListOccurences, e.ListsError)
		}
	}
}

// oldListDomains is the previous implementation of collecting the domains to
// look up in getDomainLists, kept to check that the result is unchanged.
func oldListDomains(e *Entity, domain string) []string {
	domainsToInspect := []string{domain}

	if e.checkCNAMEs && len(e.CNAME) > 0 {
		domainsToInspect = append(domainsToInspect, e.CNAME...)
	}

	var domains []string
	if e.resolveSubDomainLists {
		for i, domain := range domainsToInspect {
			var subdomains []string
			if i == 0 {
				d := strings.TrimSuffix(domain, ".")
				suffix, icann := publicsuffix.PublicSuffix(d)
				subdomains = splitDomainWithSuffix(domain, d, suffix, icann)
			} else {
				subdomains = splitDomain(domain)
			}
			domains = append(domains, subdomains...)
		}
	} else {
		domains = domainsToInspect
	}

	return makeDistinct(domains)
}

func TestAppendListDomainsMatchesOld(t *testing.T) {
	t.Parallel()

	domains := []string{
		testDomain, testParentDomain, "org.", ".", "",
		"a.b.c.d.example.co.uk.", "co.uk.", "foo.blogspot.com.", "blogspot.com.",
		"www.example.com", "...com", "a..example.com.", ".example.com.",
		"localhost.", "x.localhost.", "a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p.q.r.example.org.",
	}
	cnameSets := [][]string{
		nil,
		{},
		{testCNAME},
		{testCNAME, "edge.example-cdn.net.", testDomain},
		{testParentDomain, "a..b.example.org.", "net."},
	}
	for _, domain := range domains {
		for _, cnames := range cnameSets {
			for _, subDomains := range []bool{false, true} {
				for _, checkCNAMEs := range []bool{false, true} {
					e := &Entity{
						Domain:                domain,
						CNAME:                 cnames,
						resolveSubDomainLists: subDomains,
						checkCNAMEs:           checkCNAMEs,
					}
					want := oldListDomains(e, domain)
					var buf [16]string
					got := makeDistinct(e.appendListDomains(buf[:0], domain))
					if !slices.Equal(got, want) {
						t.Errorf("domain %q, CNAMEs %q, sub-domains %v, CNAME check %v: got %q, want %q",
							domain, cnames, subDomains, checkCNAMEs, got, want)
					}
				}
			}
		}
	}
}
