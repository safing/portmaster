package resolver

import (
	"context"
	"flag"
	"sync"
	"testing"
	"time"

	"github.com/safing/portbase/log"

	"github.com/miekg/dns"
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
	start := time.Now()
	_, err := rc.Query(silencingTraceCtx, q)
	if err != nil {
		t.Logf("client failed: %s", err) //nolint:staticcheck
		wg.Done()
		return
	}
	t.Logf("resolved %s in %s", q.FQDN, time.Since(start))
	wg.Done()
}

func TestSingleResolving(t *testing.T) {
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
	t.Logf("running bulk query test with resolver %s", resolver.Server)

	started := time.Now()

	wg := &sync.WaitGroup{}
	wg.Add(100)
	for i := 0; i < 100; i++ {
		startQuery(t, wg, resolver.Conn, &Query{ //nolint:staticcheck
			FQDN:  <-domainFeed,
			QType: dns.Type(dns.TypeA),
		})
	}
	wg.Wait()

	t.Logf("total time taken: %s", time.Since(started))
}

func TestBulkResolving(t *testing.T) {
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
	t.Logf("running bulk query test with resolver %s", resolver.Server)

	started := time.Now()

	wg := &sync.WaitGroup{}
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go startQuery(t, wg, resolver.Conn, &Query{ //nolint:staticcheck
			FQDN:  <-domainFeed,
			QType: dns.Type(dns.TypeA),
		})
	}
	wg.Wait()

	t.Logf("total time taken: %s", time.Since(started))
}
