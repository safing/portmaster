package intel

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/publicsuffix"
)

var publicSuffixTestDomains = []string{
	"", ".", "..", "...",
	"com.", "COM", "localhost", "localhost.", "notatld", "a.notatld.",
	"example.com", testDomain, "a.b.c.d.example.com.",
	"WWW.Example.COM.", "Example.Com",
	"example.com..", "www.example.com...", ".example.com", ".example.com.",
	"www..example.com.", "a...b.example.com", "..example.com.",
	"co.uk", "co.uk.", "bbc.co.uk.", "www.bbc.co.uk.", "www.BBC.CO.UK",
	"github.io", "github.io.", "user.github.io.", "www.user.github.io",
	"blogspot.com.", "a.b.c.blogspot.com.", "foo.appspot.com",
	"bucket.s3.amazonaws.com.", "s3.amazonaws.com",
	"ck", "x.ck.", "www.ck.", "a.www.ck", "a.b.x.ck.",
	"kyoto.jp", "a.kyoto.jp.", "b.a.kyoto.jp", "city.kawasaki.jp.", "x.city.kawasaki.jp.",
	"xn--bcher-kva.de.", "www.xn--bcher-kva.example.", "bücher.de", "www.bücher.de.",
	"xn--fiqs8s", "www.xn--fiqs8s.", "www.example.xn--fiqs8s.", "\u4f8b\u5b50.\u4e2d\u56fd.",
	"1.2.3.4", "1.2.3.4.", "::1", "2001:db8::1", "[::1]",
	"*.example.com.", "-.example.com", "a b.example.com", "a/b.com.",
	testCNAME, "edge-12.eu-west-1.cdn.example.co.uk.",
	strings.Repeat("a.", 60) + "example.com.",
}

// TestEntityPublicSuffixMatchesPublicSuffix checks that the public suffix
// kept by the entity gives the same results as looking it up every time,
// also when the domain of the entity changes.
func TestEntityPublicSuffixMatchesPublicSuffix(t *testing.T) {
	t.Parallel()

	reused := &Entity{}
	for _, firstHeuristics := range []bool{false, true} {
		for _, domain := range publicSuffixTestDomains {
			for _, e := range []*Entity{{}, reused} {
				e.Domain = domain
				if firstHeuristics {
					checkEffectiveTLDPlusOne(t, e)
				}

				// Like getDomainLists.
				d := strings.TrimSuffix(domain, ".")
				suffix, icann := e.getPublicSuffix(d)
				wantSuffix, wantICANN := publicsuffix.PublicSuffix(d)
				if suffix != wantSuffix || icann != wantICANN {
					t.Errorf("public suffix of %q: got %q %v, want %q %v", d, suffix, icann, wantSuffix, wantICANN)
				}
				if got, want := splitDomainWithSuffix(domain, d, suffix, icann), splitDomain(domain); !slices.Equal(got, want) {
					t.Errorf("split %q: got %q, want %q", domain, got, want)
				}

				checkEffectiveTLDPlusOne(t, e)
			}
		}
	}
}

func checkEffectiveTLDPlusOne(t *testing.T, e *Entity) {
	t.Helper()

	got, ok := e.EffectiveTLDPlusOne()
	want, wantErr := publicsuffix.EffectiveTLDPlusOne(strings.TrimRight(e.Domain, "."))
	if got != want || ok != (wantErr == nil) {
		t.Errorf("eTLD+1 of %q: got %q %v, want %q %v", e.Domain, got, ok, want, wantErr)
	}
}
