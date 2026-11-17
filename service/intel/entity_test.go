package intel

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/publicsuffix"
)

var splitDomainTestCases = [][]string{
	// Regular registered domains and subdomains.
	{"example.com."},
	{"www.example.com.", "example.com."},
	{"sub.domain.example.com.", "domain.example.com.", "example.com."},
	{"example.co.uk."},
	{"www.example.co.uk.", "example.co.uk."},

	// TLD or public suffix: Return as is.
	{"com."},
	{"googleapis.com."},

	// Public suffix domains: Return including public suffix.
	{"test.googleapis.com.", "googleapis.com."},
	{"sub.domain.googleapis.com.", "domain.googleapis.com.", "googleapis.com."},
}

func TestSplitDomain(t *testing.T) {
	t.Parallel()

	for _, testCase := range splitDomainTestCases {
		splitted := splitDomain(testCase[0])
		assert.Equal(t, testCase, splitted, "result must match")
	}
}

// TestSplitDomainMatchesGeneralImplementation checks that slicing the parent
// domains from the domain returns exactly what joining the labels returns,
// also for irregular domains.
func TestSplitDomainMatchesGeneralImplementation(t *testing.T) {
	t.Parallel()

	labels := []string{"", "a", "www", "example", "com", "co", "uk", "googleapis", "blogspot", "xn--bcher-kva", "ck", "*", "!www", "compute", "amazonaws", "cn", "local", "1", "EXAMPLE", "Com"}
	domains := make([]string, 0, 110000)
	for _, l1 := range labels {
		domains = append(domains, l1)
		for _, l2 := range labels {
			domains = append(domains, l1+"."+l2)
			for _, l3 := range labels {
				domains = append(domains, l1+"."+l2+"."+l3)
				for _, l4 := range []string{"", "com", "uk", "ck", "cn", "www"} {
					domains = append(domains, l1+"."+l2+"."+l3+"."+l4)
					domains = append(domains, "sub.domain."+l1+"."+l2+"."+l3+"."+l4)
				}
			}
		}
	}

	var sliced int
	for _, domain := range domains {
		for _, domain := range []string{domain, domain + ".", domain + "..", "." + domain} {
			d := strings.TrimSuffix(domain, ".")
			suffix, icann := publicsuffix.PublicSuffix(d)
			expected := []string{domain}
			if suffix != d {
				expected = splitIrregularDomain(d, suffix, icann)
			}

			splitted := splitDomain(domain)
			if !assert.Equal(t, expected, splitted, "domain %q", domain) {
				return
			}
			if len(splitted) > 1 {
				sliced++
			}
		}
	}
	assert.Greater(t, sliced, 10000)
}

func BenchmarkSplitDomain(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		splitDomain("www.example.com.")
		splitDomain("edge-12.eu-west-1.cdn.example.co.uk.")
	}
}

func TestGetCountryInfoWithoutLocation(t *testing.T) {
	t.Parallel()

	// Neither an entity without IP nor one with a non-global IP gets a location
	// or a location error, so no geoip database is involved.
	for name, e := range map[string]*Entity{
		"no IP":         {},
		"non-global IP": {IP: net.ParseIP("192.168.1.1")},
	} {
		e.Init(0)
		assert.NotPanics(t, func() {
			assert.Nil(t, e.GetCountryInfo(context.Background()), name)
		}, name)
	}
}
