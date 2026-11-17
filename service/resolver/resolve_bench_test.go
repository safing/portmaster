package resolver

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// benchActiveResolver returns an active resolver, so that cached entries pass
// the resolver check in checkCache.
func benchActiveResolver(b *testing.B) *Resolver {
	b.Helper()

	resolversLock.RLock()
	defer resolversLock.RUnlock()
	for _, r := range globalResolvers {
		if _, ok := activeResolvers[r.Info.ID()]; ok {
			return r
		}
	}
	b.Skip("no active resolver configured")
	return nil
}

// benchRRCache returns a typical answer: a CNAME followed by several A or AAAA
// records.
func benchRRCache(domain string, qtype uint16, resolver *Resolver) *RRCache {
	rrCache := &RRCache{
		Domain:   domain,
		Question: dns.Type(qtype),
		RCode:    dns.RcodeSuccess,
		Resolver: resolver.Info.Copy(),
	}
	target := "edge." + domain
	rrCache.Answer = append(rrCache.Answer, &dns.CNAME{
		Hdr:    dns.RR_Header{Name: domain, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
		Target: target,
	})
	for i := range 4 {
		hdr := dns.RR_Header{Name: target, Rrtype: qtype, Class: dns.ClassINET, Ttl: 300}
		if qtype == dns.TypeAAAA {
			rrCache.Answer = append(rrCache.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.ParseIP(fmt.Sprintf("2001:db8::%d", i+1))})
		} else {
			rrCache.Answer = append(rrCache.Answer, &dns.A{Hdr: hdr, A: net.IPv4(192, 0, 2, byte(i+1))})
		}
	}
	rrCache.Expires = time.Now().Add(time.Hour).Unix()
	return rrCache
}

func benchQueries(b *testing.B, n int) []*Query {
	b.Helper()

	resolver := benchActiveResolver(b)
	queries := make([]*Query, 0, n)
	for i := range n {
		qtype := dns.TypeA
		if i%2 == 1 {
			qtype = dns.TypeAAAA
		}
		domain := fmt.Sprintf("bench-%d.cachehit.example.com.", i)
		err := benchRRCache(domain, qtype, resolver).Save()
		if err != nil {
			b.Fatal(err)
		}
		queries = append(queries, &Query{FQDN: domain, QType: dns.Type(qtype)})
	}
	return queries
}

// BenchmarkResolveCacheHit measures a cache hit through Resolve, including the
// conversion to a reply, as the nameserver does for every cached answer.
// Several domains are queried in turn, as a single repeated query triggers the
// cache reset of shouldResetCache.
func BenchmarkResolveCacheHit(b *testing.B) {
	queries := benchQueries(b, 64)
	ctx := context.Background()
	request := new(dns.Msg)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		q := queries[i%len(queries)]
		rrCache, err := Resolve(ctx, &Query{FQDN: q.FQDN, QType: q.QType})
		if err != nil {
			b.Fatal(err)
		}
		if !rrCache.ServedFromCache || len(rrCache.Answer) != 5 {
			b.Fatalf("unexpected cache result: fromCache=%v answers=%d", rrCache.ServedFromCache, len(rrCache.Answer))
		}
		request.SetQuestion(q.FQDN, uint16(q.QType))
		reply := rrCache.ReplyWithDNS(ctx, request)
		reply.Extra = append(reply.Extra, rrCache.GetExtraRRs(ctx, request)...)
		_ = rrCache.ToDNSRequestContext()
	}
}

// BenchmarkGetRRCache measures loading a cached entry and converting it from
// a NameRecord to an RRCache.
func BenchmarkGetRRCache(b *testing.B) {
	queries := benchQueries(b, 64)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		q := queries[i%len(queries)]
		rrCache, err := GetRRCache(q.FQDN, q.QType)
		if err != nil {
			b.Fatal(err)
		}
		if len(rrCache.Answer) != 5 {
			b.Fatal("unexpected answer count")
		}
	}
}

// BenchmarkRRCacheSave measures saving a freshly resolved entry.
func BenchmarkRRCacheSave(b *testing.B) {
	resolver := benchActiveResolver(b)
	caches := make([]*RRCache, 64)
	for i := range caches {
		caches[i] = benchRRCache(fmt.Sprintf("bench-%d.save.example.com.", i), dns.TypeA, resolver)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		err := caches[i%len(caches)].Save()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIPInfoUpdate measures the IPInfo update done for every resolved
// address of a DNS answer (see firewall.SaveIPsInCache): get, add the domain
// and save.
func BenchmarkIPInfoUpdate(b *testing.B) {
	resolver := benchActiveResolver(b)
	rrCache := benchRRCache("ipinfo.example.com.", dns.TypeA, resolver)
	record := ResolvedDomain{
		Domain:            rrCache.Domain,
		CNAMEs:            []string{"edge.ipinfo.example.com."},
		Resolver:          rrCache.Resolver,
		DNSRequestContext: rrCache.ToDNSRequestContext(),
		Expires:           rrCache.Expires,
	}
	ips := make([]string, 64)
	for i := range ips {
		ips[i] = net.IPv4(198, 51, 100, byte(i)).String()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		ip := ips[i%len(ips)]
		info, err := GetIPInfo("bench-profile", ip)
		if err != nil {
			info = &IPInfo{IP: ip, ProfileID: "bench-profile"}
		}
		info.AddDomain(record)
		err = info.Save()
		if err != nil {
			b.Fatal(err)
		}
	}
}
