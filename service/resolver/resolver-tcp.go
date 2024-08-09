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
)

const (
	tcpConnectionEstablishmentTimeout = 3 * time.Second
	tcpWriteTimeout                   = 2 * time.Second
	heartbeatTimeout                  = 5 * time.Second
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
	// heartbeat is a alive-checking channel from which the resolver conn must
	// always read asap.
	heartbeat chan struct{}
	// abandoned signifies if the resolver conn has been abandoned.
	abandoned *abool.AtomicBool
}

// tcpQuery holds the query information for a tcpResolverConn.
type tcpQuery struct {
	Query    *Query
	Response chan *dns.Msg
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
	tr.Lock()
	defer tr.Unlock()

	// Check if we have a resolver.
	if tr.resolverConn != nil && tr.resolverConn.abandoned.IsNotSet() {
		// If there is one, check if it's alive!
		select {
		case tr.resolverConn.heartbeat <- struct{}{}:
			return tr.resolverConn, nil
		case <-time.After(heartbeatTimeout):
			log.Warningf("resolver: heartbeat for dns client %s failed", tr.resolver.Info.DescriptiveName())
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

	// Create a new if no active one is available.

	// Refresh the dialer in order to set an authenticated local address.
	tr.dnsClient.Dialer = &net.Dialer{
		LocalAddr: getLocalAddr("tcp"),
		Timeout:   tcpConnectionEstablishmentTimeout,
		KeepAlive: defaultClientTTL,
	}

	// Connect to server.
	conn, err := tr.dnsClient.Dial(tr.resolver.ServerAddress)
	if err != nil {
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

	// Create resolver connection.
	tr.resolverConnInstanceID++
	resolverConn := &tcpResolverConn{
		id:              tr.resolverConnInstanceID,
		conn:            conn,
		resolverInfo:    tr.resolver.Info,
		queries:         make(chan *tcpQuery, 10),
		responses:       make(chan *dns.Msg, 10),
		inFlightQueries: make(map[uint16]*tcpQuery, 10),
		heartbeat:       make(chan struct{}),
		abandoned:       abool.New(),
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

	// Get resolver connection.
	resolverConn, err := tr.getOrCreateResolverConn(ctx)
	if err != nil {
		return nil, err
	}

	// Create query request.
	tq := &tcpQuery{
		Query:    q,
		Response: make(chan *dns.Msg),
	}

	// Submit query request to live connection.
	select {
	case resolverConn.queries <- tq:
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

// shutdown cleanly shuts down the resolver connection.
// Must only be called once.
func (trc *tcpResolverConn) shutdown() {
	// Set abandoned status and close connection to the DNS server.
	trc.abandoned.Set()
	_ = trc.conn.Close()

	// Close all response channels for in-flight queries.
	for _, tq := range trc.inFlightQueries {
		close(tq.Response)
	}

	// Respond to any incoming queries for some time in order to not leave them
	// hanging longer than necessary.
	for {
		select {
		case tq := <-trc.queries:
			close(tq.Response)
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

func (trc *tcpResolverConn) handler(workerCtx *mgr.WorkerCtx) error {
	// Set up context and cleanup.
	trc.ctx, trc.cancelCtx = context.WithCancel(workerCtx.Ctx())
	defer trc.shutdown()

	// Set up variables.
	var readyToRecycle bool
	ttlTimer := time.After(defaultClientTTL)

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
			trc.assignUniqueID(msg)

			// Add query to in flight registry.
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

		}
	}
}

// assignUniqueID makes sure that ID assigned to msg is unique.
func (trc *tcpResolverConn) assignUniqueID(msg *dns.Msg) {
	// try a random ID 10000 times
	for range 10000 { // don't try forever
		_, exists := trc.inFlightQueries[msg.Id]
		if !exists {
			return // we are unique, yay!
		}
		msg.Id = dns.Id() // regenerate ID
	}
	// go through the complete space
	var id uint16
	for ; id <= (1<<16)-1; id++ { // don't try forever
		_, exists := trc.inFlightQueries[id]
		if !exists {
			msg.Id = id
			return // we are unique, yay!
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
		trc.responses <- msg
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
