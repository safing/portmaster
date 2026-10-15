package resolver

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/service/netenv"
)

func TestHasHealthyResolver(t *testing.T) {
	t.Parallel()

	healthy := newTestResolver(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53})
	failing := newTestResolver(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53})
	markFailing(t, failing)
	linkLocalUnavailable := newTestResolver(&net.UDPAddr{IP: net.IPv4(169, 254, 0, 1), Port: 53})
	linkLocalUnavailable.LinkLocalUnavailable = true

	tests := []struct {
		name      string
		resolvers []*Resolver
		want      bool
	}{
		{"no resolvers", nil, false},
		{"healthy resolver", []*Resolver{failing, healthy}, true},
		{"only failing resolvers", []*Resolver{failing, failing}, false},
		{"failing and unavailable link-local resolver", []*Resolver{failing, linkLocalUnavailable}, false},
	}
	for _, tt := range tests {
		if got := hasHealthyResolver(tt.resolvers); got != tt.want {
			t.Errorf("%s: hasHealthyResolver() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestResolveAndCacheAsksFailingResolvers checks that a failing resolver is
// asked when no other resolver can be asked, instead of answering nothing.
// Which resolvers are skipped is covered by TestHasHealthyResolver.
//
// The test queries the online check domain, which is resolved even while the
// device is offline, so the result does not depend on the network of the test
// machine. The online status check of the running module may resolve the same
// domain at the same time, so the resolver is checked to be asked at least
// once.
func TestResolveAndCacheAsksFailingResolvers(t *testing.T) { //nolint:paralleltest // Replaces the global resolvers.
	t.Run("only resolver is failing and answers", func(t *testing.T) {
		addr, queries := startTestDNSServer(t, true)
		failing := newTestResolver(addr)
		markFailing(t, failing)
		useGlobalResolvers(t, failing)

		rrCache, err := resolveTestQuery(t, 5*time.Second)
		if err != nil {
			t.Fatalf("query failed: %s", err)
		}
		if rrCache.RCode != dns.RcodeSuccess || len(rrCache.Answer) == 0 {
			t.Errorf("expected an answer, got RCode %s with %d records", dns.RcodeToString[rrCache.RCode], len(rrCache.Answer))
		}
		if queries.Load() == 0 {
			t.Error("failing resolver was not asked")
		}
		if failing.Conn.IsFailing() {
			t.Error("failing state was not cleared by the answer")
		}
	})

	t.Run("only resolver is failing and does not answer", func(t *testing.T) {
		addr, queries := startTestDNSServer(t, false)
		failing := newTestResolver(addr)
		markFailing(t, failing)
		useGlobalResolvers(t, failing)

		_, err := resolveTestQuery(t, 200*time.Millisecond)
		if err == nil {
			t.Fatal("expected an error, got an answer")
		}
		if errors.Is(err, ErrNotFound) {
			t.Errorf("expected a failure, got %q, which is answered as NXDOMAIN", err)
		}
		if queries.Load() == 0 {
			t.Error("failing resolver was not asked")
		}
	})
}

// resolveTestQuery resolves the online check domain through the global
// resolvers, without caching.
func resolveTestQuery(t *testing.T, timeout time.Duration) (*RRCache, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	q := &Query{
		FQDN:      netenv.DNSTestDomain,
		QType:     dns.Type(dns.TypeA),
		NoCaching: true,
	}
	if !q.check() {
		t.Fatalf("invalid query for %s", q.FQDN)
	}
	return resolveAndCache(ctx, q, nil)
}

// newTestResolver returns a plain DNS resolver for the given address. It is
// assigned by the operating system, so that clearing its failing state does
// not change the global netenv.ConnectedToDNS flag.
func newTestResolver(addr *net.UDPAddr) *Resolver {
	resolver := &Resolver{
		ServerAddress: addr.String(),
		Info: &ResolverInfo{
			Name:   "test",
			Type:   ServerTypeDNS,
			Source: ServerSourceOperatingSystem,
			IP:     addr.IP,
			Port:   uint16(addr.Port), //nolint:gosec // Ports of net.Addr are within the uint16 range.
		},
	}
	resolver.Conn = NewPlainResolver(resolver)
	return resolver
}

// markFailing marks the resolver as failing, independent of the online
// status, which ReportFailure depends on.
func markFailing(t *testing.T, resolver *Resolver) {
	t.Helper()

	conn, ok := resolver.Conn.(*PlainResolver)
	if !ok {
		t.Fatalf("unexpected resolver connection type %T", resolver.Conn)
	}
	conn.failing.Set()
}

// useGlobalResolvers replaces the global resolvers for the duration of the
// test.
func useGlobalResolvers(t *testing.T, resolvers ...*Resolver) {
	t.Helper()

	resolversLock.Lock()
	previous := globalResolvers
	globalResolvers = resolvers
	resolversLock.Unlock()

	t.Cleanup(func() {
		resolversLock.Lock()
		defer resolversLock.Unlock()
		globalResolvers = previous
	})
}

// startTestDNSServer starts a local DNS server for the duration of the test.
// If answer is true, it answers every query with the expected IP of the online
// check domain, otherwise it never answers. It returns the server address and
// the number of queries received.
func startTestDNSServer(t *testing.T, answer bool) (*net.UDPAddr, *atomic.Int32) {
	t.Helper()

	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %s", err)
	}
	addr, ok := pc.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected local address type %T", pc.LocalAddr())
	}

	queries := new(atomic.Int32)
	started := make(chan struct{})
	server := &dns.Server{
		PacketConn:        pc,
		NotifyStartedFunc: func() { close(started) },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
			queries.Add(1)
			if !answer || len(query.Question) == 0 {
				return
			}

			reply := new(dns.Msg).SetReply(query)
			reply.Answer = append(reply.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   query.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: netenv.DNSTestExpectedIP,
			})
			if err := w.WriteMsg(reply); err != nil {
				t.Errorf("failed to send reply: %s", err)
			}
		}),
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ActivateAndServe() }()
	select {
	case <-started:
	case err := <-serveErr:
		t.Fatalf("failed to start test DNS server: %s", err)
	}
	t.Cleanup(func() {
		if err := server.Shutdown(); err != nil {
			t.Errorf("failed to stop test DNS server: %s", err)
		}
	})

	return addr, queries
}
