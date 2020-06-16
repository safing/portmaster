package resolver

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func testTCPQuery(t *testing.T, wg *sync.WaitGroup, rc ResolverConn, q *Query) {
	start := time.Now()
	_, err := rc.Query(context.TODO(), q)
	if err != nil {
		t.Logf("client failed: %s", err) //nolint:staticcheck
		wg.Done()
		return
	}
	t.Logf("resolved %s in %s", q.FQDN, time.Since(start))
	wg.Done()
}

func TestTCPResolver(t *testing.T) {
	// skip if short - this test depends on the Internet and might fail randomly
	if testing.Short() {
		t.Skip()
	}

	go func() {
		time.Sleep(15 * time.Second)
		fmt.Fprintln(os.Stderr, "===== TAKING TOO LONG FOR SHUTDOWN =====")
		printStackTo(os.Stderr)
		os.Exit(1)
	}()

	// create separate resolver for this test
	resolver, _, err := createResolver("dot://9.9.9.9:853?verify=dns.quad9.net&name=Quad9&blockedif=empty", "config")
	// resolver, _, err := createResolver("dot://1.1.1.2:853?verify=cloudflare-dns.com&name=Cloudflare&blockedif=zeroip", "config")
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()

	wg := &sync.WaitGroup{}
	wg.Add(100)
	for i := 0; i < 100; i++ {
		go testTCPQuery(t, wg, resolver.Conn, &Query{ //nolint:staticcheck
			FQDN:  <-domainFeed,
			QType: dns.Type(dns.TypeA),
		})
	}
	wg.Wait()

	t.Logf("time taken: %s", time.Since(started))
}

func printStackTo(writer io.Writer) {
	fmt.Fprintln(writer, "=== PRINTING TRACES ===")
	fmt.Fprintln(writer, "=== GOROUTINES ===")
	_ = pprof.Lookup("goroutine").WriteTo(writer, 1)
	fmt.Fprintln(writer, "=== BLOCKING ===")
	_ = pprof.Lookup("block").WriteTo(writer, 1)
	fmt.Fprintln(writer, "=== MUTEXES ===")
	_ = pprof.Lookup("mutex").WriteTo(writer, 1)
	fmt.Fprintln(writer, "=== END TRACES ===")
}
