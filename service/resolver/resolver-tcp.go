package resolver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/miekg/dns"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
)

const (
	tcpConnectionEstablishmentTimeout = 3 * time.Second
	tcpWriteTimeout                   = 2 * time.Second
	heartbeatTimeout                  = 5 * time.Second

	// localBindAttempts is how often connecting is retried with another
	// pre-authenticated local port when the port cannot be bound. Attempts
	// are instant, as a bind fails synchronously, and each failed port is
	// remembered, so the retries are cheap.
	localBindAttempts = 5
)

// TCPResolver is a resolver using just a single tcp connection with pipelining.
type TCPResolver struct {
	BasicResolverConn

	// dnsClient holds the connection configuration of the resolver.
	dnsClient *dns.Client
	// resolverConn holds a connection to the DNS server, including query management.
	resolverConn *tcpResolverConn
	// resolverConnInstanceID holds the current ID of the resolverConn.
	resolverConnInstanceID int
	// heartbeatTimeout defines how long the connection may take to respond
	// to an alive check before it is replaced.
	heartbeatTimeout time.Duration
	// dialSlot is held while a new connection is established, so that
	// concurrent queries share one new connection instead of each dialing
	// their own. It is a channel, so that waiting can be canceled.
	dialSlot chan struct{}
}

// tcpResolverConn represents a single connection to an upstream DNS server.
type tcpResolverConn struct {
	// ctx is the context of the tcpResolverConn.
	ctx context.Context
	// cancelCtx cancels ctx
	cancelCtx context.CancelFunc
	// id is the ID assigned to the resolver conn.
	id int
	// conn is the connection to the DNS server.
	conn *dns.Conn
	// resolverInfo holds information about the resolver to enhance error messages.
	resolverInfo *ResolverInfo
	// queries is used to submit queries to be sent to the connected DNS server.
	queries chan *tcpQuery
	// responses is used to hand the responses from the reader to the handler.
	responses chan *dns.Msg
	// inFlightQueries holds all in-flight queries of this connection.
	inFlightQueries map[uint16]*tcpQuery
	// inFlightTimeout defines after which time unanswered in-flight queries
	// are removed from inFlightQueries.
	inFlightTimeout time.Duration
	// heartbeat is a alive-checking channel from which the resolver conn must
	// always read asap.
	heartbeat chan struct{}
	// abandoned signifies if the resolver conn has been abandoned.
	abandoned *abool.AtomicBool
	// stopped is closed when the handler has stopped. Queries are not taken
	// anymore then, and the response channels of all in-flight queries are
	// already closed.
	stopped chan struct{}
}

// errTCPConnStopped is returned when the connection stopped before it took
// the query. The query was not sent and may be retried on a new connection.
var errTCPConnStopped = fmt.Errorf("%w: connection stopped before sending the query", ErrFailure)

// tcpQuery holds the query information for a tcpResolverConn.
type tcpQuery struct {
	Query    *Query
	Response chan *dns.Msg
	// expires is set when the query is sent and is only used by the handler.
	expires time.Time
}

// MakeCacheRecord creates an RRCache record from a reply.
func (tq *tcpQuery) MakeCacheRecord(reply *dns.Msg, resolverInfo *ResolverInfo) *RRCache {
	return &RRCache{
		Domain:   tq.Query.FQDN,
		Question: tq.Query.QType,
		RCode:    reply.Rcode,
		Answer:   reply.Answer,
		Ns:       reply.Ns,
		Extra:    reply.Extra,
		Resolver: resolverInfo.Copy(),
	}
}

// NewTCPResolver returns a new TPCResolver.
func NewTCPResolver(resolver *Resolver) *TCPResolver {
	newResolver := &TCPResolver{
		BasicResolverConn: BasicResolverConn{
			resolver: resolver,
		},
		dnsClient: &dns.Client{
			Net:          "tcp",
			Timeout:      defaultConnectTimeout,
			WriteTimeout: tcpWriteTimeout,
		},
		heartbeatTimeout: heartbeatTimeout,
		dialSlot:         make(chan struct{}, 1),
	}
	newResolver.BasicResolverConn.init()
	return newResolver
}

// UseTLS enabled TLS for the TCPResolver. TLS settings must be correctly configured in the Resolver.
func (tr *TCPResolver) UseTLS() *TCPResolver {
	tr.dnsClient.Net = "tcp-tls"
	tr.dnsClient.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: tr.resolver.Info.Domain,
		// TODO: use portbase rng
	}
	return tr
}

func (tr *TCPResolver) getOrCreateResolverConn(ctx context.Context) (*tcpResolverConn, error) {
	var existingConn *tcpResolverConn

	// Minimize the time we hold the lock to avoid blocking other threads.
	tr.Lock()
	if tr.resolverConn != nil && tr.resolverConn.abandoned.IsNotSet() {
		existingConn = tr.resolverConn
	}
	seenInstanceID := tr.resolverConnInstanceID
	tr.Unlock()

	// Check if we have a resolver.
	if existingConn != nil {
		// If there is one, check if it's alive!
		select {
		case existingConn.heartbeat <- struct{}{}:
			return existingConn, nil
		case <-existingConn.stopped:
			// The connection stopped after it was checked above. Create a new one.
		case <-time.After(tr.heartbeatTimeout):
			log.Warningf("resolver: heartbeat for dns client %s failed", tr.resolver.Info.DescriptiveName())
			// Abandon the connection, so that it is shut down once it handled
			// its in-flight queries, as it is replaced by a new one below.
			existingConn.abandoned.Set()
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-module.mgr.Done():
			return nil, ErrShuttingDown
		}
	} else {
		// If there is no resolver, check if we are shutting down before dialing!
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-module.mgr.Done():
			return nil, ErrShuttingDown
		default:
		}
	}

	// Establish only one new connection at a time. Concurrent queries wait
	// for it and use it.
	select {
	case tr.dialSlot <- struct{}{}:
		defer func() { <-tr.dialSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-module.mgr.Done():
		return nil, ErrShuttingDown
	}

	// Use the connection another query created while this one was waiting.
	tr.Lock()
	if tr.resolverConn != nil &&
		tr.resolverConnInstanceID != seenInstanceID &&
		tr.resolverConn.abandoned.IsNotSet() {
		existingConn = tr.resolverConn
	} else {
		existingConn = nil
	}
	tr.Unlock()
	if existingConn != nil {
		return existingConn, nil
	}

	// Do not dial a new upstream connection once the instance has started
	// shutting down. The instance shuts down before this module does, and
	// once interception has stopped a new connection could be pended by the
	// kernel extension and never complete (#2104). Existing connections are
	// still used above.
	select {
	case <-module.instance.ShuttingDown():
		return nil, ErrShuttingDown
	default:
	}

	// Create a new if no active one is available.
	conn, err := tr.dial(ctx)
	if err != nil {
		// Nothing left the host if the local port could not be bound. Do not
		// count this against the resolver or the network.
		if errors.Is(err, ErrLocalBind) {
			// Concurrent queries dial at the same time. Use the connection
			// another one has created in the meantime, if there is one.
			tr.Lock()
			newConn := tr.resolverConn
			newInstanceID := tr.resolverConnInstanceID
			tr.Unlock()
			if newConn != nil && newInstanceID != seenInstanceID && newConn.abandoned.IsNotSet() {
				return newConn, nil
			}
			return nil, err
		}

		// Hint network environment at failed connection.
		netenv.ReportFailedConnection()

		log.Debugf("resolver: failed to connect to %s: %s", tr.resolver.Info.DescriptiveName(), err)
		return nil, fmt.Errorf("%w: failed to connect to %s: %w", ErrFailure, tr.resolver.Info.DescriptiveName(), err)
	}

	// Hint network environment at successful connection.
	netenv.ReportSuccessfulConnection()

	// Log that a connection to the resolver was established.
	log.Debugf(
		"resolver: connected to %s",
		tr.resolver.Info.DescriptiveName(),
	)

	// Thread-safe resolverConn creation.
	tr.Lock()
	defer tr.Unlock()

	// Create resolver connection.
	tr.resolverConnInstanceID++
	resolverConn := &tcpResolverConn{
		id:              tr.resolverConnInstanceID,
		conn:            conn,
		resolverInfo:    tr.resolver.Info,
		queries:         make(chan *tcpQuery, 10),
		responses:       make(chan *dns.Msg, 10),
		inFlightQueries: make(map[uint16]*tcpQuery, 10),
		// Query() gives up waiting for a reply after defaultRequestTimeout.
		// Keep unanswered queries a bit longer in order to still cache late replies.
		inFlightTimeout: 2 * defaultRequestTimeout,
		heartbeat:       make(chan struct{}),
		abandoned:       abool.New(),
		stopped:         make(chan struct{}),
	}

	// Start worker.
	module.mgr.Go("dns client", resolverConn.handler)

	// Set resolver conn for reuse.
	tr.resolverConn = resolverConn

	return resolverConn, nil
}

// Query executes the given query against the resolver.
func (tr *TCPResolver) Query(ctx context.Context, q *Query) (*RRCache, error) {
	queryStarted := time.Now()

	// The connection may stop right after it was handed out to this query,
	// e.g. when it is recycled. Send the query on a new connection then.
	tq := &tcpQuery{
		Query:    q,
		Response: make(chan *dns.Msg),
	}
	reply, err := tr.query(ctx, tq)
	if errors.Is(err, errTCPConnStopped) {
		tq = &tcpQuery{
			Query:    q,
			Response: make(chan *dns.Msg),
		}
		reply, err = tr.query(ctx, tq)
	}
	if err != nil {
		return nil, err
	}

	// Check if the reply was blocked upstream.
	if tr.resolver.IsBlockedUpstream(reply) {
		return nil, &BlockedUpstreamError{tr.resolver.Info.DescriptiveName()}
	}

	// Report request duration for metrics.
	reportRequestDuration(queryStarted, tr.resolver)

	// Create RRCache from reply and return it.
	return tq.MakeCacheRecord(reply, tr.resolver.Info), nil
}

// ForceReconnect forces the resolver to re-establish the connection to the server.
func (tr *TCPResolver) ForceReconnect(ctx context.Context) {
	tr.Lock()
	defer tr.Unlock()

	// Do nothing if no connection is available.
	if tr.resolverConn == nil {
		return
	}

	// Set the abandoned to force a new connection on next request.
	// This will leave the previous connection and handler running until all requests are handled.
	tr.resolverConn.abandoned.Set()

	log.Tracer(ctx).Tracef("resolver: marked %s for reconnecting", tr.resolver)
}

// query sends the query on a connection and waits for the reply. It returns
// errTCPConnStopped if the connection stopped before it took the query.
func (tr *TCPResolver) query(ctx context.Context, tq *tcpQuery) (*dns.Msg, error) {
	// Get resolver connection.
	resolverConn, err := tr.getOrCreateResolverConn(ctx)
	if err != nil {
		return nil, err
	}

	// Submit query request to live connection.
	select {
	case resolverConn.queries <- tq:
	case <-resolverConn.stopped:
		return nil, errTCPConnStopped
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-module.mgr.Done():
		return nil, ErrShuttingDown
	case <-time.After(defaultRequestTimeout):
		return nil, ErrTimeout
	}

	// Wait for reply.
	var reply *dns.Msg
	select {
	case reply = <-tq.Response:
	case <-resolverConn.stopped:
		// The handler closed the response channels of all queries it took
		// before it stopped. Any other query is still in the queue and was
		// never sent.
		select {
		case reply = <-tq.Response:
		default:
			return nil, errTCPConnStopped
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-module.mgr.Done():
		return nil, ErrShuttingDown
	case <-time.After(defaultRequestTimeout):
		return nil, ErrTimeout
	}

	// Check if we have a reply.
	if reply == nil {
		// Resolver is shutting down. The Portmaster may be shutting down, or
		// there is a connection error.
		return nil, ErrFailure
	}
	return reply, nil
}

// dial connects to the resolver from a pre-authenticated local port, so that
// the firewall attributes the connection to Portmaster itself. A port that
// cannot be bound, e.g. because the OS reserved it, is released, marked as
// unusable and another port is tried.
func (tr *TCPResolver) dial(ctx context.Context) (*dns.Conn, error) {
	var lastErr error
	for attempt := 1; attempt <= localBindAttempts; attempt++ {
		localAddr := getLocalAddr(networkTCP)

		// Dial with a copy of the client: the shared one may be in use by
		// other goroutines and must not be changed.
		dnsClient := *tr.dnsClient
		dnsClient.Dialer = &net.Dialer{
			LocalAddr: localAddr,
			Timeout:   tcpConnectionEstablishmentTimeout,
			KeepAlive: defaultClientTTL,
		}

		// The pre-authenticated port, if there is one. The first packet
		// consumes the pre-authentication: after a successful dial it is
		// already gone. Release it in every case, so it cannot linger and be
		// used by another process when no packet was sent or intercepted.
		var localPort uint16
		if tcpAddr, ok := localAddr.(*net.TCPAddr); ok && tcpAddr != nil {
			localPort = uint16(tcpAddr.Port) //nolint:gosec // Ports of net.Addr are within the uint16 range.
		}

		conn, err := dnsClient.DialContext(ctx, tr.resolver.ServerAddress)
		if err == nil {
			if localPort != 0 {
				releaseLocalPort(networkTCP, localPort, false)
			}
			return conn, nil
		}

		bindFailed := netutils.IsLocalBindError(err)
		if localPort != 0 {
			releaseLocalPort(networkTCP, localPort, bindFailed)
		}
		if !bindFailed {
			return nil, err
		}

		lastErr = err
		log.Debugf(
			"resolver: failed to bind local port for connection to %s (attempt %d of %d): %s",
			tr.resolver.Info.DescriptiveName(), attempt, localBindAttempts, err,
		)
	}

	return nil, fmt.Errorf("%w for connection to %s: %w", ErrLocalBind, tr.resolver.Info.DescriptiveName(), lastErr)
}

// shutdown cleanly shuts down the resolver connection.
// Must only be called once.
func (trc *tcpResolverConn) shutdown() {
	// Set abandoned status and close connection to the DNS server.
	trc.abandoned.Set()
	_ = trc.conn.Close()
	// Stop the reader, which may be blocked handing a response to the
	// response queue that is not drained anymore.
	trc.cancelCtx()

	// Close all response channels for in-flight queries.
	for _, tq := range trc.inFlightQueries {
		close(tq.Response)
	}

	// Signal that no queries are taken anymore. Queries submitted before or
	// after are never sent and are retried by Query on a new connection.
	close(trc.stopped)
}

func (trc *tcpResolverConn) handler(workerCtx *mgr.WorkerCtx) error {
	// Set up context and cleanup.
	trc.ctx, trc.cancelCtx = context.WithCancel(workerCtx.Ctx())
	defer trc.shutdown()

	// Set up variables.
	var readyToRecycle bool
	ttlTimer := time.After(defaultClientTTL)
	cleanupTicker := time.NewTicker(trc.inFlightTimeout)
	defer cleanupTicker.Stop()

	// Start connection reader.
	module.mgr.Go("dns client reader", trc.reader)

	// Handle requests.
	for {
		select {
		case <-trc.heartbeat:
			// Respond to alive checks.

		case <-trc.ctx.Done():
			// Respond to module shutdown or conn error.
			return nil

		case <-ttlTimer:
			// Recycle the connection after the TTL is reached.
			readyToRecycle = true
			// Send dummy response to trigger the check.
			select {
			case trc.responses <- nil:
			default:
				// The response queue is full.
				// The check will be triggered by another response.
			}

		case tq := <-trc.queries:
			// Handle DNS query request.

			// Create dns request message.
			msg := &dns.Msg{}
			msg.SetQuestion(tq.Query.FQDN, uint16(tq.Query.QType))

			// Assign a unique message ID.
			if !trc.assignUniqueID(msg) {
				// All message IDs are in use, fail the query.
				log.Warningf(
					"resolver: no free message ID for query to %s, %d queries in flight",
					trc.resolverInfo.DescriptiveName(),
					len(trc.inFlightQueries),
				)
				close(tq.Response)
				continue
			}

			// Add query to in flight registry.
			tq.expires = time.Now().Add(trc.inFlightTimeout)
			trc.inFlightQueries[msg.Id] = tq

			// Write query to connected DNS server.
			_ = trc.conn.SetWriteDeadline(time.Now().Add(tcpWriteTimeout))
			err := trc.conn.WriteMsg(msg)
			if err != nil {
				trc.logConnectionError(err, false)
				return nil
			}

		case msg := <-trc.responses:
			if msg != nil {
				trc.handleQueryResponse(msg)
			}

			// If we are ready to recycle and we have no in-flight queries, we can
			// shutdown the connection and create a new one for the next query.
			if readyToRecycle || trc.abandoned.IsSet() {
				if len(trc.inFlightQueries) == 0 {
					log.Debugf("resolver: recycling connection to %s", trc.resolverInfo.DescriptiveName())
					return nil
				}
			}

		case <-cleanupTicker.C:
			// Remove queries that were never answered. The waiting Query() call
			// has given up already.
			trc.pruneInFlightQueries()

			if readyToRecycle || trc.abandoned.IsSet() {
				if len(trc.inFlightQueries) == 0 {
					log.Debugf("resolver: recycling connection to %s", trc.resolverInfo.DescriptiveName())
					return nil
				}
			}
		}
	}
}

// assignUniqueID makes sure that ID assigned to msg is unique.
// Returns false if all IDs are in use.
func (trc *tcpResolverConn) assignUniqueID(msg *dns.Msg) bool {
	// try a random ID 10000 times
	for range 10000 { // don't try forever
		_, exists := trc.inFlightQueries[msg.Id]
		if !exists {
			return true // we are unique, yay!
		}
		msg.Id = dns.Id() // regenerate ID
	}
	// go through the complete space
	for id := range 1 << 16 {
		_, exists := trc.inFlightQueries[uint16(id)]
		if !exists {
			msg.Id = uint16(id)
			return true // we are unique, yay!
		}
	}
	return false
}

// pruneInFlightQueries removes expired queries from the in-flight registry.
func (trc *tcpResolverConn) pruneInFlightQueries() {
	now := time.Now()
	for id, tq := range trc.inFlightQueries {
		if now.After(tq.expires) {
			delete(trc.inFlightQueries, id)
		}
	}
}

func (trc *tcpResolverConn) handleQueryResponse(msg *dns.Msg) {
	// Get in flight from registry.
	tq, ok := trc.inFlightQueries[msg.Id]
	if ok {
		delete(trc.inFlightQueries, msg.Id)
	} else {
		log.Debugf(
			"resolver: received possibly unsolicited reply from %s: txid=%d q=%+v",
			trc.resolverInfo.DescriptiveName(),
			msg.Id,
			msg.Question,
		)
		return
	}

	// Send response to waiting query handler.
	select {
	case tq.Response <- msg:
		return
	default:
		// No one is listening for that response.
	}

	// If caching is disabled for this query, we are done.
	if tq.Query.NoCaching {
		return
	}

	// Otherwise, we can persist the answer in case the request is repeated.
	rrCache := tq.MakeCacheRecord(msg, trc.resolverInfo)
	rrCache.Clean(minTTL)
	err := rrCache.Save()
	if err != nil {
		log.Warningf(
			"resolver: failed to cache RR for %s: %s",
			tq.Query.ID(),
			err,
		)
	}
}

func (trc *tcpResolverConn) reader(workerCtx *mgr.WorkerCtx) error {
	defer trc.cancelCtx()

	for {
		msg, err := trc.conn.ReadMsg()
		if err != nil {
			trc.logConnectionError(err, true)
			return nil
		}
		select {
		case trc.responses <- msg:
		case <-trc.ctx.Done():
			// The handler stopped and does not process responses anymore.
			return nil
		}
	}
}

func (trc *tcpResolverConn) logConnectionError(err error, reading bool) {
	// Check if we are the first to see an error.
	if trc.abandoned.SetToIf(false, true) {
		// Log error.
		switch {
		case errors.Is(err, io.EOF):
			log.Debugf(
				"resolver: connection to %s was closed",
				trc.resolverInfo.DescriptiveName(),
			)
		case reading:
			log.Warningf(
				"resolver: read error from %s: %s",
				trc.resolverInfo.DescriptiveName(),
				err,
			)
		default:
			log.Warningf(
				"resolver: write error to %s: %s",
				trc.resolverInfo.DescriptiveName(),
				err,
			)
		}
	}
}
