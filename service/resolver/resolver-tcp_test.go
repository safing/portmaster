package resolver

import (
	"errors"
	"net"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/tevino/abool"
)

// TestTCPResolverDialRetriesOnBindError checks that a pre-authenticated port
// that cannot be bound is released as unusable and the dial is retried with
// the next port.
func TestTCPResolverDialRetriesOnBindError(t *testing.T) { //nolint:paralleltest // Replaces the package-level hooks.
	bt := newTCPBindTest(t)
	freePort := freeTCPPort(t)
	bt.usePorts(bt.heldPort, freePort)

	conn, err := bt.resolver.dial(t.Context())
	if err != nil {
		t.Fatalf("dial failed: %s", err)
	}
	_ = conn.Close()

	// The occupied port is released as unusable, the connected port is
	// released as usable after the dial.
	expected := []releasedPort{
		{port: bt.heldPort, unusable: true},
		{port: freePort, unusable: false},
	}
	if got := bt.releasedPorts(); !slices.Equal(got, expected) {
		t.Errorf("expected releases %v, got %v", expected, got)
	}
}

// TestTCPResolverDialGivesUpOnBindErrors checks that the dial fails with
// ErrLocalBind, which continues with the next resolver, when no port can be
// bound within the allowed attempts.
func TestTCPResolverDialGivesUpOnBindErrors(t *testing.T) { //nolint:paralleltest // Replaces the package-level hooks.
	bt := newTCPBindTest(t)
	bt.usePorts(bt.heldPort)

	conn, err := bt.resolver.dial(t.Context())
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected dial to fail when no port can be bound")
	}
	if !errors.Is(err, ErrLocalBind) || !errors.Is(err, ErrContinue) {
		t.Errorf("expected ErrLocalBind wrapping ErrContinue, got: %v", err)
	}

	// Every attempt releases the occupied port as unusable.
	expected := slices.Repeat([]releasedPort{{port: bt.heldPort, unusable: true}}, localBindAttempts)
	if got := bt.releasedPorts(); !slices.Equal(got, expected) {
		t.Errorf("expected releases %v, got %v", expected, got)
	}
}

// TestTCPResolverUsesConcurrentConnOnBindError checks that a connection
// created by a concurrent query while binding fails is used instead of
// reporting the bind error.
func TestTCPResolverUsesConcurrentConnOnBindError(t *testing.T) { //nolint:paralleltest // Replaces the package-level hooks.
	bt := newTCPBindTest(t)
	bt.usePorts(bt.heldPort)
	tr := bt.resolver

	// Without a concurrent connection, the bind error is passed on.
	_, err := tr.getOrCreateResolverConn(t.Context())
	if !errors.Is(err, ErrLocalBind) {
		t.Errorf("expected ErrLocalBind, got: %v", err)
	}

	// Simulate a concurrent query that creates a connection while this one
	// fails to bind: set the connection from within the port selection, which
	// runs after getOrCreateResolverConn has noted the current connection.
	concurrentConn := &tcpResolverConn{
		abandoned: abool.New(),
		heartbeat: make(chan struct{}),
	}
	selectPort := localAddrFactory
	localAddrFactory = func(network string) net.Addr {
		tr.Lock()
		if tr.resolverConn == nil {
			tr.resolverConnInstanceID++
			concurrentConn.id = tr.resolverConnInstanceID
			tr.resolverConn = concurrentConn
		}
		tr.Unlock()
		return selectPort(network)
	}

	resolverConn, err := tr.getOrCreateResolverConn(t.Context())
	if err != nil {
		t.Fatalf("expected the concurrently created connection, got error: %s", err)
	}
	if resolverConn != concurrentConn {
		t.Error("expected the concurrently created connection to be returned")
	}
}

type releasedPort struct {
	port     uint16
	unusable bool
}

// tcpBindTest is the fixture of the bind tests: a TCP resolver pointed at a
// server that accepts connections, a port that is occupied so that binding it
// fails, and hooks that record the ports the resolver releases.
type tcpBindTest struct {
	t        *testing.T
	resolver *TCPResolver
	heldPort uint16

	lock     sync.Mutex
	released []releasedPort
}

func newTCPBindTest(t *testing.T) *tcpBindTest {
	t.Helper()

	// A server that accepts connections and closes them right away.
	server, serverPort := listenTCP(t)
	go func() {
		for {
			conn, err := server.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	// Occupy a port, so that binding it fails.
	_, heldPort := listenTCP(t)

	bt := &tcpBindTest{t: t, heldPort: heldPort}
	bt.resolver = NewTCPResolver(&Resolver{
		ServerAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(serverPort))),
		Info: &ResolverInfo{
			Name:   "test-tcp",
			Type:   ServerTypeTCP,
			Source: ServerSourceConfigured,
			IP:     net.IPv4(127, 0, 0, 1),
			Port:   serverPort,
		},
	})

	// Record the released tcp ports. The started module may run UDP queries
	// concurrently that use the same hooks, so other networks are ignored.
	previousHooks := localPortHooks
	previousFactory := localAddrFactory
	localPortHooks = LocalPortHooks{
		Authorize: func(network string, port uint16) {
			if network == networkTCP {
				t.Errorf("the tcp resolver must not authorize ports itself, got port %d", port)
			}
		},
		Release: func(network string, port uint16, unusable bool) {
			if network != networkTCP {
				return
			}
			bt.lock.Lock()
			defer bt.lock.Unlock()
			bt.released = append(bt.released, releasedPort{port: port, unusable: unusable})
		},
	}
	t.Cleanup(func() {
		localPortHooks = previousHooks
		localAddrFactory = previousFactory
	})

	return bt
}

// usePorts makes the resolver use the given local ports in order, repeating
// the last one, in place of the ports the firewall hands out.
func (bt *tcpBindTest) usePorts(ports ...uint16) {
	var next int
	localAddrFactory = func(network string) net.Addr {
		if network != networkTCP {
			bt.t.Errorf("expected network tcp, got %q", network)
		}
		bt.lock.Lock()
		defer bt.lock.Unlock()
		port := ports[min(next, len(ports)-1)]
		next++
		return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(port)}
	}
}

// releasedPorts returns the ports released so far, in order.
func (bt *tcpBindTest) releasedPorts() []releasedPort {
	bt.lock.Lock()
	defer bt.lock.Unlock()
	return slices.Clone(bt.released)
}

// listenTCP listens on a free loopback port and returns the listener, which
// is closed when the test ends, and its port.
func listenTCP(t *testing.T) (net.Listener, uint16) {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %s", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener address type %T", listener.Addr())
	}
	return listener, uint16(addr.Port) //nolint:gosec // Ports of net.Addr are within the uint16 range.
}

// freeTCPPort returns a port that is free at the time of the call.
func freeTCPPort(t *testing.T) uint16 {
	t.Helper()

	listener, port := listenTCP(t)
	_ = listener.Close()
	return port
}
