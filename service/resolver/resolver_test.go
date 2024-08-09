package resolver

import (
	"context"
	"flag"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/base/log"
)

var (
	testResolver         string
	silencingTraceCtx, _ = log.AddTracer(context.Background())
)

func init() {
	flag.StringVar(
		&testResolver,
		"resolver",
		"dot://1.1.1.2:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip",
		"set custom resolver for testing",
	)
}

func startQuery(t *testing.T, wg *sync.WaitGroup, rc ResolverConn, q *Query) {
	t.Helper()

	start := time.Now()
	_, err := rc.Query(silencingTraceCtx, q)
	if err != nil {
		t.Logf("client failed: %s", err)
		wg.Done()
		return
	}
	t.Logf("resolved %s in %s", q.FQDN, time.Since(start))
	wg.Done()
}

func TestSingleResolving(t *testing.T) {
	t.Parallel()

	// skip if short - this test depends on the Internet and might fail randomly
	if testing.Short() {
		t.Skip()
	}

	defaultRequestTimeout = 30 * time.Second

	// create separate resolver for this test
	resolver, _, err := createResolver(testResolver, "config")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("running bulk query test with resolver %s", resolver.Info.DescriptiveName())

	started := time.Now()

	wg := &sync.WaitGroup{}
	wg.Add(100)
	for range 100 {
		startQuery(t, wg, resolver.Conn, &Query{
			FQDN:  <-domainFeed,
			QType: dns.Type(dns.TypeA),
		})
	}
	wg.Wait()

	t.Logf("total time taken: %s", time.Since(started))
}

func TestBulkResolving(t *testing.T) {
	t.Parallel()

	// skip if short - this test depends on the Internet and might fail randomly
	if testing.Short() {
		t.Skip()
	}

	defaultRequestTimeout = 30 * time.Second

	// create separate resolver for this test
	resolver, _, err := createResolver(testResolver, "config")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("running bulk query test with resolver %s", resolver.Info.DescriptiveName())

	started := time.Now()

	wg := &sync.WaitGroup{}
	wg.Add(100)
	for range 100 {
		go startQuery(t, wg, resolver.Conn, &Query{
			FQDN:  <-domainFeed,
			QType: dns.Type(dns.TypeA),
		})
	}
	wg.Wait()

	t.Logf("total time taken: %s", time.Since(started))
}

func TestPublicSuffix(t *testing.T) {
	t.Parallel()

	testSuffix(t, "co.uk.", "", true)
	testSuffix(t, "amazon.co.uk.", "amazon.co.uk.", true)
	testSuffix(t, "books.amazon.co.uk.", "amazon.co.uk.", true)
	testSuffix(t, "www.books.amazon.co.uk.", "amazon.co.uk.", true)
	testSuffix(t, "com.", "", true)
	testSuffix(t, "amazon.com.", "amazon.com.", true)
	testSuffix(t, "example0.debian.net.", "example0.debian.net.", true)
	testSuffix(t, "example1.debian.org.", "debian.org.", true)
	testSuffix(t, "golang.dev.", "golang.dev.", true)
	testSuffix(t, "golang.net.", "golang.net.", true)
	testSuffix(t, "play.golang.org.", "golang.org.", true)
	testSuffix(t, "gophers.in.space.museum.", "space.museum.", true)
	testSuffix(t, "0emm.com.", "0emm.com.", true)
	testSuffix(t, "a.0emm.com.", "", true)
	testSuffix(t, "b.c.d.0emm.com.", "c.d.0emm.com.", true)
	testSuffix(t, "org.", "", true)
	testSuffix(t, "foo.org.", "foo.org.", true)
	testSuffix(t, "foo.co.uk.", "foo.co.uk.", true)
	testSuffix(t, "foo.dyndns.org.", "foo.dyndns.org.", true)
	testSuffix(t, "foo.blogspot.co.uk.", "foo.blogspot.co.uk.", true)
	testSuffix(t, "there.is.no.such-tld.", "no.such-tld.", false)
	testSuffix(t, "www.some.bit.", "some.bit.", false)
	testSuffix(t, "cromulent.", "", false)
	testSuffix(t, "arpa.", "", true)
	testSuffix(t, "in-addr.arpa.", "", true)
	testSuffix(t, "1.in-addr.arpa.", "1.in-addr.arpa.", true)
	testSuffix(t, "ip6.arpa.", "", true)
	testSuffix(t, "1.ip6.arpa.", "1.ip6.arpa.", true)
	testSuffix(t, "www.some.arpa.", "some.arpa.", true)
	testSuffix(t, "www.some.home.arpa.", "home.arpa.", true)
	testSuffix(t, ".", "", false)
	testSuffix(t, "", "", false)

	// Test edge case domains.
	testSuffix(t, "www.some.example.", "some.example.", true)
	testSuffix(t, "www.some.invalid.", "some.invalid.", true)
	testSuffix(t, "www.some.local.", "some.local.", true)
	testSuffix(t, "www.some.localhost.", "some.localhost.", true)
	testSuffix(t, "www.some.onion.", "some.onion.", false)
	testSuffix(t, "www.some.test.", "some.test.", true)
}

func testSuffix(t *testing.T, fqdn, domainRoot string, icannSpace bool) {
	t.Helper()

	q := &Query{FQDN: fqdn}
	q.InitPublicSuffixData()
	assert.Equal(t, domainRoot, q.DomainRoot)
	assert.Equal(t, icannSpace, q.ICANNSpace)
}
