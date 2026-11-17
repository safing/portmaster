package resolver

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestTCPResolverWaitersShareFailedDial checks that queries waiting for
// another query to establish a connection get the failure of that dial,
// instead of each dialing again one after another.
func TestTCPResolverWaitersShareFailedDial(t *testing.T) {
	t.Parallel()

	// The server fails the TLS handshake only after a while, like a server
	// that does not respond and makes the dial fail after the connect
	// timeout. The first connection is held until the test releases it.
	const dialDuration = 300 * time.Millisecond
	server := newSlowFailingDoTServer(t, dialDuration)
	tr := NewTCPResolver(server.resolver()).UseTLS()

	// Query concurrently while the first dial is in progress.
	const queries = 6
	errs := make(chan error, queries)
	for i := range queries {
		go func() {
			_, err := tr.Query(t.Context(), &Query{
				FQDN:      "q" + strconv.Itoa(i) + ".example.",
				QType:     dns.Type(dns.TypeA),
				NoCaching: true,
			})
			errs <- err
		}()
	}
	server.waitForConns(t, 1)
	// Give the other queries time to wait for the dial.
	time.Sleep(500 * time.Millisecond)

	// Fail the first dial: all waiting queries must fail with it right away.
	released := time.Now()
	server.releaseFirst()
	for range queries {
		err := <-errs
		if !errors.Is(err, ErrFailure) {
			t.Errorf("expected connection failure, got: %v", err)
		}
	}
	took := time.Since(released)
	if got := server.acceptedConns(); got != 1 {
		t.Errorf("expected 1 dial for %d concurrent queries, got %d", queries, got)
	}
	// Dialing one after another takes (queries-1) * dialDuration = 1.5s.
	if took > time.Second {
		t.Errorf("waiting queries failed only after %s", took)
	}

	// A query after the failed dial dials again.
	_, err := tr.Query(t.Context(), &Query{FQDN: "later.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	if !errors.Is(err, ErrFailure) {
		t.Errorf("expected connection failure, got: %v", err)
	}
	if got := server.acceptedConns(); got != 2 {
		t.Errorf("expected a new dial for a later query, got %d dials in total", got)
	}
}

// TestTCPResolverWaitersDialAfterCanceledDial checks that queries waiting
// for a dial that failed because the dialing query was canceled dial
// themselves instead of failing with that error.
func TestTCPResolverWaitersDialAfterCanceledDial(t *testing.T) {
	t.Parallel()

	server := newSlowFailingDoTServer(t, 50*time.Millisecond)
	tr := NewTCPResolver(server.resolver()).UseTLS()

	// Start a dial with a query that is canceled while dialing.
	ctx, cancel := context.WithCancel(t.Context())
	firstErr := make(chan error, 1)
	go func() {
		_, err := tr.Query(ctx, &Query{FQDN: "canceled.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
		firstErr <- err
	}()
	server.waitForConns(t, 1)

	// Queue more queries behind it.
	const waiting = 2
	errs := make(chan error, waiting)
	for i := range waiting {
		go func() {
			_, err := tr.Query(t.Context(), &Query{
				FQDN:      "w" + strconv.Itoa(i) + ".example.",
				QType:     dns.Type(dns.TypeA),
				NoCaching: true,
			})
			errs <- err
		}()
	}
	time.Sleep(200 * time.Millisecond)

	cancel()
	server.releaseFirst()
	<-firstErr
	for range waiting {
		err := <-errs
		if !errors.Is(err, ErrFailure) || errors.Is(err, context.Canceled) {
			t.Errorf("expected connection failure of an own dial, got: %v", err)
		}
	}
	// One waiting query dials again, the other one waits for that dial and
	// fails with it.
	if got := server.acceptedConns(); got != 2 {
		t.Errorf("expected the waiting queries to dial again once, got %d dials in total", got)
	}
}

// slowFailingDoTServer is a DNS-over-TLS server that accepts connections but
// closes them after a delay, so that the TLS handshake and thereby the dial
// fails only then. The first connection is held until releaseFirst is called.
type slowFailingDoTServer struct {
	port    uint16
	release chan struct{}
	once    sync.Once

	lock     sync.Mutex
	accepted int
}

func newSlowFailingDoTServer(t *testing.T, failAfter time.Duration) *slowFailingDoTServer {
	t.Helper()

	listener, port := listenTCP(t)
	s := &slowFailingDoTServer{port: port, release: make(chan struct{})}
	t.Cleanup(s.releaseFirst)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			s.lock.Lock()
			s.accepted++
			first := s.accepted == 1
			s.lock.Unlock()
			go func() {
				if first {
					<-s.release
				} else {
					time.Sleep(failAfter)
				}
				_ = conn.Close()
			}()
		}
	}()
	return s
}

func (s *slowFailingDoTServer) resolver() *Resolver {
	return &Resolver{
		ServerAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(s.port))),
		Info: &ResolverInfo{
			Name:   "test-dot-failing",
			Type:   ServerTypeDoT,
			Source: ServerSourceConfigured,
			IP:     net.IPv4(127, 0, 0, 1),
			Domain: "dot.example",
			Port:   s.port,
		},
	}
}

// releaseFirst makes the first connection fail.
func (s *slowFailingDoTServer) releaseFirst() {
	s.once.Do(func() { close(s.release) })
}

// acceptedConns returns how many connections the server accepted so far.
func (s *slowFailingDoTServer) acceptedConns() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.accepted
}

// waitForConns waits until the server accepted n connections.
func (s *slowFailingDoTServer) waitForConns(t *testing.T, n int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for s.acceptedConns() < n {
		if time.Now().After(deadline) {
			t.Fatalf("server accepted only %d of %d connections", s.acceptedConns(), n)
		}
		time.Sleep(time.Millisecond)
	}
}
