package customlists

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/publicsuffix"
)

// splitDomainReference is the previous implementation of splitDomain.
func splitDomainReference(domain string) []string {
	domain = strings.Trim(domain, ".")
	suffix, _ := publicsuffix.PublicSuffix(domain)
	if suffix == domain {
		return []string{domain}
	}

	domainWithoutSuffix := domain[:len(domain)-len(suffix)]
	domainWithoutSuffix = strings.Trim(domainWithoutSuffix, ".")

	splitted := strings.FieldsFunc(domainWithoutSuffix, func(r rune) bool {
		return r == '.'
	})

	domains := make([]string, 0, len(splitted))
	for idx := range splitted {

		d := strings.Join(splitted[idx:], ".") + "." + suffix
		if d[len(d)-1] != '.' {
			d += "."
		}
		domains = append(domains, d)
	}
	return domains
}

func checkSplitDomain(t *testing.T, domain string) {
	t.Helper()
	want := splitDomainReference(domain)
	got := splitDomain(domain)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("splitDomain(%q) = %q, want %q", domain, got, want)
	}
}

var splitDomainTable = []string{
	"", ".", "..", "...", "com", "com.", ".com", ".com.", "co.uk", "github.io", "foo.github.io",
	"example.com", "example.com.", ".example.com", "..example.com..", "www.example.com",
	"a.b.c.d.e.example.com.", "a.b.co.uk", "a.b.co.uk.", "a..b.com", "a..b.com.", "a.b..com",
	"a.com..", "..a.com", "A.B.COM", "Foo.GitHub.IO", "xn--bcher-kva.example", "www.xn--bcher-kva.de",
	"foo.unknowntld", "a.b.unknowntld.", "localhost", "a.localhost", "x.y.z.s3.amazonaws.com",
	"foo.bar.blogspot.com", "a.b.kobe.jp", "city.kobe.jp", "a.ck", "www.ck", "1.2.3.4", "a. b.com",
	"a.b.com.\t", "e\u00e9.com", "a.b.\u30b3\u30e0",
}

func TestSplitDomainMatchesReference(t *testing.T) {
	t.Parallel()
	for _, domain := range splitDomainTable {
		checkSplitDomain(t, domain)
	}
}

func TestSplitDomainMatchesReferenceRandom(t *testing.T) {
	t.Parallel()
	labels := []string{
		"a", "b", "www", "example", "foo", "bar", "A", "XN--bcher-kva", "xn--p1ai", "com", "org", "uk", "co",
		"github", "io", "blogspot", "amazonaws", "s3", "jp", "kobe", "ck", "unknowntld", "localhost", "", "-", "1",
	}
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // Deterministic test data.
	for range 100000 {
		var sb strings.Builder
		for range rng.Intn(3) {
			if rng.Intn(4) == 0 {
				sb.WriteByte('.')
			}
		}
		n := rng.Intn(7)
		for i := range n {
			if i > 0 {
				sb.WriteByte('.')
				if rng.Intn(15) == 0 {
					sb.WriteByte('.')
				}
			}
			sb.WriteString(labels[rng.Intn(len(labels))])
		}
		for range rng.Intn(3) {
			if rng.Intn(3) == 0 {
				sb.WriteByte('.')
			}
		}
		checkSplitDomain(t, sb.String())
	}
}

var splitDomainBenchDomains = []string{"www.example.com.", "a.b.c.example.co.uk.", "foo.github.io."}

func BenchmarkSplitDomain(b *testing.B) {
	b.ReportAllocs()
	for i := range b.N {
		_ = splitDomain(splitDomainBenchDomains[i%len(splitDomainBenchDomains)])
	}
}

func BenchmarkSplitDomainReference(b *testing.B) {
	b.ReportAllocs()
	for i := range b.N {
		_ = splitDomainReference(splitDomainBenchDomains[i%len(splitDomainBenchDomains)])
	}
}
