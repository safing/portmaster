package resolver

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
)

func TestGetRRCacheParsedCopies(t *testing.T) {
	t.Parallel()

	domain := "parsed-copies.example.com."
	saved := &RRCache{
		Domain:   domain,
		Question: dns.Type(dns.TypeA),
		RCode:    dns.RcodeSuccess,
		Answer: []dns.RR{
			&dns.CNAME{Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 17}, Target: "edge.example.net."},
			&dns.A{Hdr: dns.RR_Header{Name: "edge.example.net.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 17}, A: net.IPv4(192, 0, 2, 1)},
		},
		Ns: []dns.RR{
			&dns.NS{Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 17}, Ns: "ns.parsed-copies.example.com."},
		},
		Expires:  time.Now().Add(time.Hour).Unix(),
		Resolver: &ResolverInfo{Type: ServerTypeDNS},
	}
	err := saved.Save()
	if err != nil {
		t.Fatal(err)
	}

	// The first call parses the entries, the second uses the parsed records.
	first, err := GetRRCache(domain, dns.Type(dns.TypeA))
	if err != nil {
		t.Fatal(err)
	}
	second, err := GetRRCache(domain, dns.Type(dns.TypeA))
	if err != nil {
		t.Fatal(err)
	}
	for _, rrCache := range []*RRCache{first, second} {
		assert.Equal(t, saved.Answer, rrCache.Answer)
		assert.Equal(t, saved.Ns, rrCache.Ns)
		assert.Nil(t, rrCache.Extra)
	}

	// Changes to returned records must not affect later results.
	second.ReplaceAnswerNames("PARSED-COPIES.example.com.")
	aRecord, ok := second.Answer[1].(*dns.A)
	if !ok {
		t.Fatal("expected A record")
	}
	aRecord.A[len(aRecord.A)-1] = 99
	aRecord.Hdr.Ttl = 1
	third, err := GetRRCache(domain, dns.Type(dns.TypeA))
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, saved.Answer, third.Answer)
	assert.Equal(t, saved.Ns, third.Ns)
}

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
