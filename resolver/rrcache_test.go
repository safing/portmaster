package resolver

import (
	"testing"

	"github.com/miekg/dns"
)

func TestCaching(t *testing.T) {
	t.Parallel()

	testDomain := "Mk35mMqOWEHXSMk11MYcbjLOjTE8PQvDiAVUxf4BvwtgR.example.com."
	testQuestion := "A"

	testNameRecord := &NameRecord{
		Domain:   testDomain,
		Question: testQuestion,
		Resolver: &ResolverInfo{
			Type: "dns",
		},
	}

	err := testNameRecord.Save()
	if err != nil {
		t.Fatal(err)
	}

	rrCache, err := GetRRCache(testDomain, dns.Type(dns.TypeA))
	if err != nil {
		t.Fatal(err)
	}

	err = rrCache.Save()
	if err != nil {
		t.Fatal(err)
	}

	rrCache2, err := GetRRCache(testDomain, dns.Type(dns.TypeA))
	if err != nil {
		t.Fatal(err)
	}

	if rrCache2.Domain != rrCache.Domain {
		t.Fatal("something very is wrong")
	}
}
