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

func TestOrderResolvers(t *testing.T) {
	t.Parallel()

	healthy1 := newTestResolver(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53})
	healthy2 := newTestResolver(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 53})
	failing1 := newTestResolver(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 3), Port: 53})
	markFailing(t, failing1)
	failing2 := newTestResolver(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 4), Port: 53})
	markFailing(t, failing2)

	tests := []struct {
		name      string
		resolvers []*Resolver
		want      []*Resolver
	}{
		{"no resolvers", nil, nil},
		{"only healthy resolvers", []*Resolver{healthy1, healthy2}, []*Resolver{healthy1, healthy2}},
		{"only failing resolvers", []*Resolver{failing1, failing2}, []*Resolver{failing1, failing2}},
		{"failing resolver already last", []*Resolver{healthy1, healthy2, failing1}, []*Resolver{healthy1, healthy2, failing1}},
		{"failing resolvers are moved last", []*Resolver{failing1, healthy1, failing2, healthy2}, []*Resolver{healthy1, healthy2, failing1, failing2}},
	}
	for _, tt := range tests {
		input := append([]*Resolver(nil), tt.resolvers...)
		got := orderResolvers(tt.resolvers)
		for i := range input {
			if tt.resolvers[i] != input[i] {
				t.Errorf("%s: orderResolvers() modified the given slice at index %d", tt.name, i)
				break
			}
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s: orderResolvers() returned %d resolvers, want %d", tt.name, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: orderResolvers()[%d] = %s, want %s", tt.name, i, got[i].ServerAddress, tt.want[i].ServerAddress)
			}
		}
	}
}

// TestResolveAndCacheAsksFailingResolvers checks that a failing resolver is
// asked after the other resolvers did not answer, instead of never, and that
// it is not asked while another resolver answers.
//
// The test queries the online check domain, which is resolved even while the
// device is offline, so the result does not depend on the network of the test
// machine. The online status check of the running module may resolve the same
// domain at the same time, so the resolver is checked to be asked at least
// once.
func TestResolveAndCacheAsksFailingResolvers(t *testing.T) { //nolint:paralleltest // Replaces the global resolvers.
	// Runs first: it needs the online status, and the resolvers of the other
	// subtests do not answer the online check of the running module.
	t.Run("failing resolver is not asked after a not found answer", func(t *testing.T) {
		// Special-use domains are tried on all local resolvers, and a "not
		// found" answer continues with the next one.
		healthyAddr, healthyQueries := startTestDNSServer(t, dns.RcodeNameError)
		healthy := newTestResolver(healthyAddr)
		failingAddr, failingQueries := startTestDNSServer(t, dns.RcodeSuccess)
		failing := newTestResolver(failingAddr)
		markFailing(t, failing)
		useTestResolvers(t, healthy, failing)
		requireOnline(t)

		rrCache, err := resolveTestQuery(t, "host.lan.")
		if err != nil {
			t.Fatalf("query failed: %s", err)
		}
		if rrCache.RCode != dns.RcodeNameError {
			t.Errorf("expected NXDOMAIN, got RCode %s", dns.RcodeToString[rrCache.RCode])
		}
		if healthyQueries.Load() == 0 {
			t.Error("healthy resolver was not asked")
		}
		if failingQueries.Load() != 0 {
			t.Errorf("failing resolver was asked %d times", failingQueries.Load())
		}
		if !failing.Conn.IsFailing() {
			t.Error("failing state was cleared without the resolver being asked")
		}
	})

	t.Run("failing resolver is not asked while a healthy one answers", func(t *testing.T) {
		healthyAddr, healthyQueries := startTestDNSServer(t, dns.RcodeSuccess)
		healthy := newTestResolver(healthyAddr)
		failingAddr, failingQueries := startTestDNSServer(t, dns.RcodeSuccess)
		failing := newTestResolver(failingAddr)
		markFailing(t, failing)
		// The failing resolver is configured first.
		useTestResolvers(t, failing, healthy)

		rrCache, err := resolveTestQuery(t, netenv.DNSTestDomain)
		if err != nil {
			t.Fatalf("query failed: %s", err)
		}
		if rrCache.RCode != dns.RcodeSuccess || len(rrCache.Answer) == 0 {
			t.Errorf("expected an answer, got RCode %s with %d records", dns.RcodeToString[rrCache.RCode], len(rrCache.Answer))
		}
		if healthyQueries.Load() == 0 {
			t.Error("healthy resolver was not asked")
		}
		if failingQueries.Load() != 0 {
			t.Errorf("failing resolver was asked %d times", failingQueries.Load())
		}
		if !failing.Conn.IsFailing() {
			t.Error("failing state was cleared without the resolver being asked")
		}
	})

	t.Run("failing resolver is asked after the healthy one does not answer", func(t *testing.T) {
		healthyAddr, _ := startTestDNSServer(t, noReply)
		healthy := newTestResolver(healthyAddr)
		failingAddr, failingQueries := startTestDNSServer(t, dns.RcodeSuccess)
		failing := newTestResolver(failingAddr)
		markFailing(t, failing)
		useTestResolvers(t, healthy, failing)

		rrCache, err := resolveTestQuery(t, netenv.DNSTestDomain)
		if err != nil {
			t.Fatalf("query failed: %s", err)
		}
		if rrCache.RCode != dns.RcodeSuccess || len(rrCache.Answer) == 0 {
			t.Errorf("expected an answer, got RCode %s with %d records", dns.RcodeToString[rrCache.RCode], len(rrCache.Answer))
		}
		if failingQueries.Load() == 0 {
			t.Error("failing resolver was not asked")
		}
		if failing.Conn.IsFailing() {
			t.Error("failing state was not cleared by the answer")
		}
	})

	t.Run("only resolver is failing and answers", func(t *testing.T) {
		addr, queries := startTestDNSServer(t, dns.RcodeSuccess)
		failing := newTestResolver(addr)
		markFailing(t, failing)
		useTestResolvers(t, failing)

		rrCache, err := resolveTestQuery(t, netenv.DNSTestDomain)
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
		addr, _ := startTestDNSServer(t, noReply)
		failing := newTestResolver(addr)
		markFailing(t, failing)
		useTestResolvers(t, failing)

		// Without the failing resolver being asked, the result would be
		// ErrNotFound instead of the error of the query.
		_, err := resolveTestQuery(t, netenv.DNSTestDomain)
		if err == nil {
			t.Fatal("expected an error, got an answer")
		}
		if errors.Is(err, ErrNotFound) {
			t.Errorf("expected a failure, got %q, which is answered as NXDOMAIN", err)
		}
	})
}

// TestCheckFailingResolvers checks that the recovery check clears a failing
// resolver as soon as it answers, whatever the answer is, and keeps it failing
// while it does not answer.
func TestCheckFailingResolvers(t *testing.T) { //nolint:paralleltest // Replaces the global resolvers.
	tests := []struct {
		name        string
		rcode       int
		wantFailing bool
	}{
		{"answers with another IP", dns.RcodeSuccess, false},
		{"answers with NXDOMAIN", dns.RcodeNameError, false},
		{"answers with REFUSED", dns.RcodeRefused, false},
		{"answers with SERVFAIL", dns.RcodeServerFailure, false},
		{"does not answer", noReply, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := startTestDNSServer(t, tt.rcode)
			resolver := newTestResolver(addr)
			markFailing(t, resolver)
			useTestResolvers(t, resolver)

			if err := module.mgr.Do("check failing resolvers", checkFailingResolvers); err != nil {
				t.Fatalf("check failed: %s", err)
			}
			if got := resolver.Conn.IsFailing(); got != tt.wantFailing {
				t.Errorf("resolver failing = %v, want %v", got, tt.wantFailing)
			}
		})
	}
}

// resolveTestQuery resolves the given domain through the test resolvers,
// without caching.
func resolveTestQuery(t *testing.T, fqdn string) (*RRCache, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	q := &Query{
		FQDN:      fqdn,
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

// useTestResolvers replaces the global and local resolvers for the duration
// of the test.
//
// It also clears the resolver config, so that the test resolvers never count
// as "all configured resolvers failing". Otherwise resolving would send a
// notification, and the notifications module is not running in these tests.
func useTestResolvers(t *testing.T, resolvers ...*Resolver) {
	t.Helper()

	resolversLock.Lock()
	previousGlobal, previousLocal, previousConfig := globalResolvers, localResolvers, currentResolverConfig
	globalResolvers, localResolvers, currentResolverConfig = resolvers, resolvers, nil
	resolversLock.Unlock()

	t.Cleanup(func() {
		resolversLock.Lock()
		defer resolversLock.Unlock()
		globalResolvers, localResolvers, currentResolverConfig = previousGlobal, previousLocal, previousConfig
	})
}

// requireOnline makes sure the device counts as online for the duration of
// the test. Queries other than the online check query are not resolved while
// offline, and the test resolvers of other tests do not answer the online
// check of the running module.
func requireOnline(t *testing.T) {
	t.Helper()

	wasConnected := netenv.ConnectedToDNS.IsSet()
	netenv.ConnectedToDNS.Set()
	t.Cleanup(func() { netenv.ConnectedToDNS.SetTo(wasConnected) })

	// A check that is already running returns its own, older result first.
	// Check again until the result reflects the connected DNS.
	deadline := time.Now().Add(30 * time.Second)
	for {
		status := netenv.CheckAndGetOnlineStatus()
		if status == netenv.StatusOnline {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected online status %s, got %s", netenv.StatusOnline, status)
		}
	}
}

// noReply makes startTestDNSServer return an address nobody listens on.
const noReply = -1

// startTestDNSServer starts a local DNS server for the duration of the test.
// It answers every query with the given RCode, adding 127.0.0.1 to successful
// answers. That is not the expected IP of the online check domain. It returns
// the server address and the number of queries received.
//
// With noReply, no server is started: queries to the address fail at once
// instead of waiting for a timeout. A waiting query, for example of the online
// status check of the running module, could still be running after the test.
func startTestDNSServer(t *testing.T, rcode int) (*net.UDPAddr, *atomic.Int32) {
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
	if rcode == noReply {
		if err := pc.Close(); err != nil {
			t.Fatalf("failed to close listener: %s", err)
		}
		return addr, queries
	}

	started := make(chan struct{})
	server := &dns.Server{
		PacketConn:        pc,
		NotifyStartedFunc: func() { close(started) },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
			queries.Add(1)
			if len(query.Question) == 0 {
				return
			}

			reply := new(dns.Msg).SetRcode(query, rcode)
			if rcode == dns.RcodeSuccess {
				reply.Answer = append(reply.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   query.Question[0].Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    60,
					},
					A: net.IPv4(127, 0, 0, 1),
				})
			}
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
