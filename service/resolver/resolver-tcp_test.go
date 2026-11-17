package resolver

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
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

// TestTCPResolverUsesConcurrentConnOnBindError checks that the bind error is
// passed on, and that a query waiting for a concurrent query to establish a
// connection uses that connection instead of failing to bind a port itself.
//
// A connection can only be created while holding the dial slot, so it cannot
// appear while a query that holds the slot is binding.
func TestTCPResolverUsesConcurrentConnOnBindError(t *testing.T) { //nolint:paralleltest // Replaces the package-level hooks.
	bt := newTCPBindTest(t)
	bt.usePorts(bt.heldPort)
	tr := bt.resolver

	// Without a concurrent connection, the bind error is passed on.
	_, err := tr.getOrCreateResolverConn(t.Context())
	if !errors.Is(err, ErrLocalBind) {
		t.Errorf("expected ErrLocalBind, got: %v", err)
	}

	// Simulate a concurrent query that holds the dial slot and creates a
	// connection while this one waits for the slot.
	concurrentConn := &tcpResolverConn{
		abandoned: abool.New(),
		heartbeat: make(chan struct{}),
	}
	// Answer alive checks, in case the connection is already set when the
	// query looks for one.
	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go func() {
		for {
			select {
			case <-concurrentConn.heartbeat:
			case <-stopHeartbeat:
				return
			}
		}
	}()
	tr.dialSlot <- struct{}{}
	type result struct {
		conn *tcpResolverConn
		err  error
	}
	results := make(chan result, 1)
	go func() {
		conn, err := tr.getOrCreateResolverConn(t.Context())
		results <- result{conn, err}
	}()
	time.Sleep(50 * time.Millisecond)
	tr.Lock()
	tr.resolverConnInstanceID++
	concurrentConn.id = tr.resolverConnInstanceID
	tr.resolverConn = concurrentConn
	tr.Unlock()
	<-tr.dialSlot

	res := <-results
	if res.err != nil {
		t.Fatalf("expected the concurrently created connection, got error: %s", res.err)
	}
	if res.conn != concurrentConn {
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
	previousHooks := localPortHooks.Load()
	previousFactory := localAddrFactory.Load()
	localPortHooks.Store(&LocalPortHooks{
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
	})
	t.Cleanup(func() {
		localPortHooks.Store(previousHooks)
		localAddrFactory.Store(previousFactory)
	})

	return bt
}

// usePorts makes the resolver use the given local ports in order, repeating
// the last one, in place of the ports the firewall hands out.
func (bt *tcpBindTest) usePorts(ports ...uint16) {
	var next int
	factory := func(network string) net.Addr {
		if network != networkTCP {
			bt.t.Errorf("expected network tcp, got %q", network)
		}
		bt.lock.Lock()
		defer bt.lock.Unlock()
		port := ports[min(next, len(ports)-1)]
		next++
		return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(port)}
	}
	localAddrFactory.Store(&factory)
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

// TestTCPResolverConnUnansweredQuery checks that a query the server never
// answers does not stay in the in-flight registry forever, which would keep
// an abandoned connection (and its handler and reader) alive indefinitely.
func TestTCPResolverConnUnansweredQuery(t *testing.T) {
	t.Parallel()

	clientSide, serverSide := net.Pipe()
	defer func() {
		_ = serverSide.Close()
	}()
	trc := &tcpResolverConn{
		conn:            &dns.Conn{Conn: clientSide},
		resolverInfo:    &ResolverInfo{Name: "PipeTest", Type: ServerTypeTCP},
		queries:         make(chan *tcpQuery, 10),
		responses:       make(chan *dns.Msg, 10),
		inFlightQueries: make(map[uint16]*tcpQuery, 10),
		inFlightTimeout: 100 * time.Millisecond,
		heartbeat:       make(chan struct{}),
		abandoned:       abool.New(),
		stopped:         make(chan struct{}),
	}
	module.mgr.Go("test dns client", trc.handler)

	// Submit a query and receive it on the server side, but never answer it.
	trc.queries <- &tcpQuery{
		Query:    &Query{FQDN: "unanswered.example.", QType: dns.Type(dns.TypeA), NoCaching: true},
		Response: make(chan *dns.Msg),
	}
	server := &dns.Conn{Conn: serverSide}
	_ = serverSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := server.ReadMsg()
	if err != nil {
		t.Fatalf("server failed to read query: %s", err)
	}

	// Abandon the connection, like ForceReconnect does.
	trc.abandoned.Set()

	// The handler must close the connection once the query expired.
	_ = serverSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = server.ReadMsg()
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("abandoned connection with unanswered query was not closed: %v", err)
	}
}

// TestTCPAssignUniqueIDExhausted checks that assigning a message ID terminates
// when all 65536 IDs are in flight.
func TestTCPAssignUniqueIDExhausted(t *testing.T) {
	t.Parallel()

	trc := &tcpResolverConn{
		inFlightQueries: make(map[uint16]*tcpQuery, 1<<16),
	}
	for id := range 1 << 16 {
		trc.inFlightQueries[uint16(id)] = &tcpQuery{}
	}

	done := make(chan bool, 1)
	go func() {
		done <- trc.assignUniqueID(&dns.Msg{})
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("assignUniqueID reported success although all IDs are in use")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("assignUniqueID did not return with all IDs in use")
	}

	// With exactly one free ID, it must be found.
	delete(trc.inFlightQueries, 4242)
	msg := &dns.Msg{}
	if !trc.assignUniqueID(msg) || msg.Id != 4242 {
		t.Fatalf("expected to get free ID 4242, got ID %d", msg.Id)
	}
}

// TestTCPResolverConnReaderExitsWithFullResponseQueue checks that the reader
// of a connection terminates when the handler stops while the reader is
// blocked handing a response to the full response queue. Nobody drains the
// queue after the handler stopped, so the reader would otherwise block
// forever and keep the connection, its buffers and its worker alive.
func TestTCPResolverConnReaderExitsWithFullResponseQueue(t *testing.T) {
	t.Parallel()

	clientSide, serverSide := net.Pipe()
	defer func() {
		_ = serverSide.Close()
	}()
	trc := &tcpResolverConn{
		conn:            &dns.Conn{Conn: clientSide},
		resolverInfo:    &ResolverInfo{Name: "PipeTest", Type: ServerTypeTCP},
		queries:         make(chan *tcpQuery, 10),
		responses:       make(chan *dns.Msg, 10),
		inFlightQueries: make(map[uint16]*tcpQuery, 10),
		inFlightTimeout: time.Minute,
		heartbeat:       make(chan struct{}),
		abandoned:       abool.New(),
		stopped:         make(chan struct{}),
	}
	// Readers blocked on a response queue that do not belong to this test.
	foreignReaders := blockedTCPReaders()
	module.mgr.Go("test dns client", trc.handler)

	// Submit a query and read only the start of it on the server side, so that
	// the handler is stuck writing it and does not process responses.
	trc.queries <- &tcpQuery{
		Query:    &Query{FQDN: "flood.example.", QType: dns.Type(dns.TypeA), NoCaching: true},
		Response: make(chan *dns.Msg, 1),
	}
	_ = serverSide.SetDeadline(time.Now().Add(5 * time.Second))
	_, err := io.ReadFull(serverSide, make([]byte, 2))
	if err != nil {
		t.Fatalf("server failed to read query: %s", err)
	}

	// Send more responses than the response queue holds. With the pipe, a
	// write returns only after the reader read it completely, so the reader
	// then blocks handing the last response to the full queue.
	server := &dns.Conn{Conn: serverSide}
	for i := range cap(trc.responses) + 1 {
		reply := &dns.Msg{}
		reply.SetQuestion("flood.example.", dns.TypeA)
		reply.Id = uint16(i) //nolint:gosec // Small loop counter.
		reply.Response = true
		err = server.WriteMsg(reply)
		if err != nil {
			t.Fatalf("server failed to write response %d: %s", i, err)
		}
	}
	var readers []string
	deadline := time.Now().Add(5 * time.Second)
	for len(readers) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("reader did not block on the full response queue")
		}
		time.Sleep(time.Millisecond)
		for _, id := range blockedTCPReaders() {
			if !slices.Contains(foreignReaders, id) {
				readers = append(readers, id)
			}
		}
	}

	// Fail the pending write by closing the server side: the handler stops
	// and shuts down the connection.
	_ = serverSide.Close()

	// The reader must terminate.
	deadline = time.Now().Add(5 * time.Second)
	for {
		stacks := allGoroutineStacks()
		var leaked []string
		for _, id := range readers {
			if strings.Contains(stacks, "goroutine "+id+" [") {
				leaked = append(leaked, id)
			}
		}
		if len(leaked) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("reader goroutine(s) %v leaked after the connection was shut down", leaked)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// blockedTCPReaders returns the goroutine IDs of the tcp resolver connection
// readers that are blocked handing a response to the response queue, i.e.
// that are blocked in the reader function itself and not in reading.
func blockedTCPReaders() []string {
	var ids []string
	for g := range strings.SplitSeq(allGoroutineStacks(), "\n\n") {
		header, frames, _ := strings.Cut(g, "\n")
		if !strings.Contains(header, "[chan send") && !strings.Contains(header, "[select") {
			continue
		}
		// Find the innermost frame outside of the runtime.
		var topFrame string
		for line := range strings.SplitSeq(frames, "\n") {
			if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "runtime.") {
				topFrame = line
				break
			}
		}
		if !strings.Contains(topFrame, "/service/resolver.(*tcpResolverConn).reader(") {
			continue
		}
		fields := strings.Fields(header)
		if len(fields) >= 2 {
			ids = append(ids, fields[1])
		}
	}
	return ids
}

// allGoroutineStacks returns the stacks of all goroutines.
func allGoroutineStacks() string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// TestTCPResolverQueryOnStoppedConn checks that a query handed to a
// connection whose handler stopped after the query obtained the connection
// does not wait for a response that can never arrive, but is sent on a new
// connection.
func TestTCPResolverQueryOnStoppedConn(t *testing.T) {
	t.Parallel()

	server := newTCPTestServer(t)
	tr := NewTCPResolver(server.resolver())

	// Establish a connection with a first query.
	_, err := tr.Query(t.Context(), &Query{FQDN: "first.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	if err != nil {
		t.Fatalf("first query failed: %s", err)
	}
	tr.Lock()
	staleConn := tr.resolverConn
	tr.Unlock()

	// Close the connection on the server side: the handler stops.
	server.closeConns()
	deadline := time.Now().Add(5 * time.Second)
	for staleConn.abandoned.IsNotSet() {
		if time.Now().After(deadline) {
			t.Fatal("connection was not abandoned after the server closed it")
		}
		time.Sleep(time.Millisecond)
	}
	// Let the shutdown of the handler complete.
	time.Sleep(300 * time.Millisecond)

	// Recreate the state of a query that obtained the connection right before
	// it stopped: the connection is not abandoned yet and the heartbeat
	// succeeds. Abandon it afterwards, as the shutdown does.
	staleConn.abandoned.UnSet()
	go func() {
		select {
		case <-staleConn.heartbeat:
			staleConn.abandoned.Set()
		case <-time.After(5 * time.Second):
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	rrCache, err := tr.Query(ctx, &Query{FQDN: "second.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	took := time.Since(started)
	if err != nil {
		t.Fatalf("query on stopped connection failed after %s: %s", took, err)
	}
	if len(rrCache.Answer) != 1 {
		t.Errorf("expected one answer, got %v", rrCache.Answer)
	}
	if took > time.Second {
		t.Errorf("query on stopped connection took %s", took)
	}
}

// TestTCPResolverAbandonsConnWithFailedHeartbeat checks that a connection
// that failed the alive check and was replaced is shut down once it handled
// its in-flight queries, instead of running until its TTL.
func TestTCPResolverAbandonsConnWithFailedHeartbeat(t *testing.T) {
	t.Parallel()

	server := newTCPTestServer(t)
	tr := NewTCPResolver(server.resolver())
	tr.heartbeatTimeout = 50 * time.Millisecond

	// Set up a connection whose server side is controlled by the test.
	clientSide, serverSide := net.Pipe()
	defer func() {
		_ = serverSide.Close()
	}()
	oldConn := &tcpResolverConn{
		conn:            &dns.Conn{Conn: clientSide},
		resolverInfo:    tr.resolver.Info,
		queries:         make(chan *tcpQuery, 10),
		responses:       make(chan *dns.Msg, 10),
		inFlightQueries: make(map[uint16]*tcpQuery, 10),
		inFlightTimeout: time.Minute,
		heartbeat:       make(chan struct{}),
		abandoned:       abool.New(),
		stopped:         make(chan struct{}),
	}
	tr.Lock()
	tr.resolverConnInstanceID++
	oldConn.id = tr.resolverConnInstanceID
	tr.resolverConn = oldConn
	tr.Unlock()
	module.mgr.Go("test dns client", oldConn.handler)

	// Submit a query and read only the start of it on the server side, so that
	// the handler is stuck writing it and does not respond to alive checks.
	oldConn.queries <- &tcpQuery{
		Query:    &Query{FQDN: "slow.example.", QType: dns.Type(dns.TypeA), NoCaching: true},
		Response: make(chan *dns.Msg, 1),
	}
	_ = serverSide.SetDeadline(time.Now().Add(5 * time.Second))
	lengthPrefix := make([]byte, 2)
	_, err := io.ReadFull(serverSide, lengthPrefix)
	if err != nil {
		t.Fatalf("server failed to read query: %s", err)
	}

	// The heartbeat fails, and the query is sent on a new connection.
	_, err = tr.Query(t.Context(), &Query{FQDN: "new.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	if err != nil {
		t.Fatalf("query on new connection failed: %s", err)
	}
	tr.Lock()
	replaced := tr.resolverConn != oldConn
	tr.Unlock()
	if !replaced {
		t.Fatal("connection with failed heartbeat was not replaced")
	}

	// Let the old connection complete its pending query.
	packed := make([]byte, int(lengthPrefix[0])<<8|int(lengthPrefix[1]))
	_, err = io.ReadFull(serverSide, packed)
	if err != nil {
		t.Fatalf("server failed to read query: %s", err)
	}
	query := &dns.Msg{}
	err = query.Unpack(packed)
	if err != nil {
		t.Fatalf("server failed to unpack query: %s", err)
	}
	reply := &dns.Msg{}
	reply.SetReply(query)
	serverConn := &dns.Conn{Conn: serverSide}
	err = serverConn.WriteMsg(reply)
	if err != nil {
		t.Fatalf("server failed to write reply: %s", err)
	}

	// The old connection must be closed now that it has no in-flight queries.
	_ = serverSide.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = serverConn.ReadMsg()
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("replaced connection was not closed: %v", err)
	}
}

// tcpTestServer is a DNS server on a loopback TCP port that answers every A
// query with 127.0.0.1.
type tcpTestServer struct {
	port uint16

	lock     sync.Mutex
	conns    []net.Conn
	accepted int
}

func newTCPTestServer(t *testing.T) *tcpTestServer {
	t.Helper()

	listener, port := listenTCP(t)
	s := &tcpTestServer{port: port}
	t.Cleanup(s.closeConns)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			s.lock.Lock()
			s.conns = append(s.conns, conn)
			s.accepted++
			s.lock.Unlock()
			go s.serve(&dns.Conn{Conn: conn})
		}
	}()
	return s
}

func (s *tcpTestServer) resolver() *Resolver {
	return &Resolver{
		ServerAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(s.port))),
		Info: &ResolverInfo{
			Name:   "test-tcp-server",
			Type:   ServerTypeTCP,
			Source: ServerSourceConfigured,
			IP:     net.IPv4(127, 0, 0, 1),
			Port:   s.port,
		},
	}
}

func (s *tcpTestServer) serve(conn *dns.Conn) {
	for {
		query, err := conn.ReadMsg()
		if err != nil {
			return
		}
		reply := &dns.Msg{}
		reply.SetReply(query)
		reply.Answer = append(reply.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.IPv4(127, 0, 0, 1),
		})
		if conn.WriteMsg(reply) != nil {
			return
		}
	}
}

// closeConns closes all connections accepted so far.
func (s *tcpTestServer) closeConns() {
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = nil
}

// acceptedConns returns how many connections the server accepted so far.
func (s *tcpTestServer) acceptedConns() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.accepted
}

// TestTCPResolverConcurrentReconnectDialsOnce checks that concurrent queries
// that find the connection broken establish a single new connection that
// they all use, instead of each dialing its own and orphaning all but the
// last one.
func TestTCPResolverConcurrentReconnectDialsOnce(t *testing.T) {
	t.Parallel()

	server := newTCPTestServer(t)
	tr := NewTCPResolver(server.resolver())

	// Establish a connection with a first query.
	_, err := tr.Query(t.Context(), &Query{FQDN: "first.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	if err != nil {
		t.Fatalf("first query failed: %s", err)
	}
	tr.Lock()
	brokenConn := tr.resolverConn
	tr.Unlock()

	// Break the connection on the server side and wait for the handler to stop.
	server.closeConns()
	select {
	case <-brokenConn.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("connection did not stop after the server closed it")
	}

	// Query concurrently.
	const queries = 20
	start := make(chan struct{})
	errs := make(chan error, queries)
	for i := range queries {
		go func() {
			<-start
			_, err := tr.Query(t.Context(), &Query{
				FQDN:      "q" + strconv.Itoa(i) + ".example.",
				QType:     dns.Type(dns.TypeA),
				NoCaching: true,
			})
			errs <- err
		}()
	}
	close(start)
	for range queries {
		err := <-errs
		if err != nil {
			t.Errorf("concurrent query failed: %s", err)
		}
	}

	// Exactly one new connection must have been established.
	if got := server.acceptedConns(); got != 2 {
		t.Errorf("expected 1 new connection for %d concurrent queries, got %d", queries, got-1)
	}
}

// TestTCPResolverWaitForDialHonorsContext checks that a query waiting for
// another query to establish a connection gives up when its context ends.
func TestTCPResolverWaitForDialHonorsContext(t *testing.T) {
	t.Parallel()

	server := newTCPTestServer(t)
	tr := NewTCPResolver(server.resolver())

	// Simulate a dial in progress by another query.
	tr.dialSlot <- struct{}{}
	defer func() { <-tr.dialSlot }()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := tr.Query(ctx, &Query{FQDN: "waiting.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context deadline error, got: %v", err)
	}
	if took := time.Since(started); took > time.Second {
		t.Errorf("waiting query gave up only after %s", took)
	}
	if got := server.acceptedConns(); got != 0 {
		t.Errorf("expected no connection, got %d", got)
	}
}

// TestTCPResolverRecyclesBusyConn checks that a connection that always has a
// query in flight is still recycled after its TTL.
func TestTCPResolverRecyclesBusyConn(t *testing.T) {
	t.Parallel()

	// A server that answers every query after a delay, so that queries
	// overlap. It records for each accepted connection whether the client
	// closed it.
	listener, port := listenTCP(t)
	var (
		lock   sync.Mutex
		closed []bool
	)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			lock.Lock()
			index := len(closed)
			closed = append(closed, false)
			lock.Unlock()
			go func() {
				dnsConn := &dns.Conn{Conn: conn}
				var writeLock sync.Mutex
				for {
					query, err := dnsConn.ReadMsg()
					if err != nil {
						lock.Lock()
						closed[index] = true
						lock.Unlock()
						return
					}
					go func() {
						time.Sleep(100 * time.Millisecond)
						reply := &dns.Msg{}
						reply.SetReply(query)
						writeLock.Lock()
						defer writeLock.Unlock()
						_ = dnsConn.WriteMsg(reply)
					}()
				}
			}()
		}
	}()
	server := &tcpTestServer{port: port}
	tr := NewTCPResolver(server.resolver())
	tr.clientTTL = 200 * time.Millisecond

	// Keep queries in flight all the time.
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			time.Sleep(time.Duration(i) * 25 * time.Millisecond)
			for ctx.Err() == nil {
				_, _ = tr.Query(ctx, &Query{FQDN: "busy.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
			}
		})
	}

	// The first connection must be replaced and closed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		lock.Lock()
		recycled := len(closed) >= 2 && closed[0]
		accepted := len(closed)
		lock.Unlock()
		if recycled {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			wg.Wait()
			t.Fatalf("busy connection was not recycled 10x past its TTL (%d connections accepted)", accepted)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
}

// TestTCPResolverConnReplyBeforeQueryWaits checks that a reply that the
// handler receives before the query waits for it is not dropped. The query
// hands itself to the handler through a buffered queue and may not wait for
// the reply yet when the reply arrives, e.g. under CPU load. The query then
// timed out although the server answered.
func TestTCPResolverConnReplyBeforeQueryWaits(t *testing.T) {
	t.Parallel()

	trc := &tcpResolverConn{
		resolverInfo:    &ResolverInfo{Name: "ReplyTest", Type: ServerTypeTCP},
		inFlightQueries: make(map[uint16]*tcpQuery, 1),
	}
	newReply := func(tq *tcpQuery, id uint16) *dns.Msg {
		trc.inFlightQueries[id] = tq
		reply := &dns.Msg{}
		reply.SetQuestion(tq.Query.FQDN, dns.TypeA)
		reply.Id = id
		reply.Response = true
		return reply
	}

	// The reply arrives before the query waits for it.
	tq := newTCPQuery(&Query{FQDN: "early.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	reply := newReply(tq, 1)
	trc.handleQueryResponse(reply)
	select {
	case got := <-tq.Response:
		if got != reply {
			t.Errorf("expected the reply, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("reply that arrived before the query waited for it was dropped")
	}

	// The reply arrives right before the query stops waiting: it is used.
	tq = newTCPQuery(&Query{FQDN: "late.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	reply = newReply(tq, 2)
	trc.handleQueryResponse(reply)
	if got := tq.stopWaiting(); got != reply {
		t.Errorf("expected the delivered reply when giving up, got %v", got)
	}

	// The reply arrives after the query stopped waiting: it is not delivered.
	tq = newTCPQuery(&Query{FQDN: "gone.example.", QType: dns.Type(dns.TypeA), NoCaching: true})
	if got := tq.stopWaiting(); got != nil {
		t.Errorf("expected no reply, got %v", got)
	}
	trc.handleQueryResponse(newReply(tq, 3))
	if len(tq.Response) != 0 {
		t.Error("reply was delivered to a query that stopped waiting")
	}
}
