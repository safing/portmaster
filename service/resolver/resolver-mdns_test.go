package resolver

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestMDNSQueryReceivesResponse checks that a mDNS response is handed to the
// waiting query right away, instead of the query running into its timeout.
// Run with -race: queries use the unicast connections set up by the mDNS
// listener worker.
func TestMDNSQueryReceivesResponse(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// Run a separate message handler, so that we can inject a response.
	messages := make(chan *dns.Msg)
	go func() {
		_ = handleMDNSMessages(ctx, messages)
	}()

	q := &Query{
		FQDN:  "portmaster-mdns-test.local.",
		QType: dns.Type(dns.TypeA),
	}

	type queryResult struct {
		rrCache *RRCache
		err     error
		took    time.Duration
	}
	resultCh := make(chan queryResult, 1)
	started := time.Now()
	go func() {
		rrCache, err := queryMulticastDNS(ctx, q)
		resultCh <- queryResult{rrCache, err, time.Since(started)}
	}()

	// Answer the query, like a mDNS responder on the network would, until the
	// query returns.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case res := <-resultCh:
			if res.err != nil {
				if strings.Contains(res.err.Error(), "not initialized") ||
					strings.Contains(res.err.Error(), "failed to send query") {
					t.Skipf("mDNS not available here: %s", res.err)
				}
				t.Fatal(res.err)
			}
			if res.rrCache.RCode != dns.RcodeSuccess || len(res.rrCache.Answer) == 0 {
				t.Fatalf("unexpected result: rcode=%d answers=%d", res.rrCache.RCode, len(res.rrCache.Answer))
			}
			// The query waits 1s for a response before falling back to the cache.
			if res.took > 900*time.Millisecond {
				t.Fatalf("query took %s although a response was available immediately", res.took)
			}
			return

		case <-ticker.C:
			var (
				id    uint16
				found bool
			)
			questionsLock.Lock()
			for msgID, savedQ := range questions {
				if savedQ.question.Name == q.FQDN {
					id = msgID
					found = true
				}
			}
			questionsLock.Unlock()
			if !found {
				continue
			}

			reply := new(dns.Msg)
			reply.Id = id
			reply.Response = true
			reply.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: q.FQDN, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.IPv4(192, 168, 1, 1),
			}}
			select {
			case messages <- reply:
			case <-time.After(time.Second):
				t.Fatal("message handler is stuck")
			}

		case <-time.After(5 * time.Second):
			t.Fatal("query did not return")
		}
	}
}
