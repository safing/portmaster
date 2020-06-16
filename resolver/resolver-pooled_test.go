package resolver

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func testQuery(t *testing.T, wg *sync.WaitGroup, newCnt *uint32, brc *BasicResolverConn, q *Query) {
	dnsClient := brc.clientManager.getDNSClient()

	// create query
	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(q.FQDN, uint16(q.QType))

	// get connection
	conn, new, err := dnsClient.getConn()
	if err != nil {
		t.Logf("failed to connect: %s", err) //nolint:staticcheck
		wg.Done()
		return
	}
	if new {
		atomic.AddUint32(newCnt, 1)
	}

	// query server
	reply, ttl, err := dnsClient.client.ExchangeWithConn(dnsQuery, conn)
	if err != nil {
		t.Logf("client failed: %s", err) //nolint:staticcheck
		wg.Done()
		return
	}
	if reply == nil {
		t.Fatalf("resolved %s, but reply was empty!", q.FQDN) //nolint:staticcheck
	}

	t.Logf("resolved %s [new resolver = %v] in %s", q.FQDN, new, ttl)
	dnsClient.addToPool()
	wg.Done()
}

func TestClientPooling(t *testing.T) {
	// skip if short - this test depends on the Internet and might fail randomly
	if testing.Short() {
		t.Skip()
	}

	// create separate resolver for this test
	resolver, _, err := createResolver("dot://9.9.9.9:853?verify=dns.quad9.net&name=Quad9&blockedif=empty", "config")
	// resolver, _, err := createResolver("dot://1.1.1.2:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip", "config")
	if err != nil {
		t.Fatal(err)
	}
	brc := &BasicResolverConn{
		clientManager: clientManagerFactory(resolver.ServerType)(resolver),
		resolver:      resolver,
	}
	resolver.Conn = brc

	started := time.Now()

	wg := &sync.WaitGroup{}
	var newCnt uint32
	for i := 0; i < 10; i++ {
		wg.Add(10)
		for j := 0; j < 10; j++ {
			go testQuery(t, wg, &newCnt, brc, &Query{ //nolint:staticcheck
				FQDN:  <-domainFeed,
				QType: dns.Type(dns.TypeA),
			})
		}
		wg.Wait()
		if newCnt > uint32(10+i) {
			t.Fatalf("unexpected pool size: %d (limit is %d)", newCnt, 10+i)
		}
	}

	t.Logf("time taken: %s", time.Since(started))
}
