package resolver

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
	"github.com/tevino/abool"
)

const (
	tcpWriteTimeout    = 2 * time.Second
	ignoreQueriesAfter = 10 * time.Minute
	heartbeatTimeout   = 15 * time.Second
)

// TCPResolver is a resolver using just a single tcp connection with pipelining.
type TCPResolver struct {
	BasicResolverConn

	clientTTL time.Duration
	dnsClient *dns.Client

	clientStarted   *abool.AtomicBool
	clientHeartbeat chan struct{}
	stopClient      func()
	connInstanceID  *uint32
	queries         chan *dns.Msg
	inFlightQueries map[uint16]*InFlightQuery
}

// InFlightQuery represents an in flight query of a TCPResolver.
type InFlightQuery struct {
	Query          *Query
	Msg            *dns.Msg
	Response       chan *dns.Msg
	Resolver       *Resolver
	Started        time.Time
	ConnInstanceID uint32
}

// MakeCacheRecord creates an RCache record from a reply.
func (ifq *InFlightQuery) MakeCacheRecord(reply *dns.Msg) *RRCache {
	return &RRCache{
		Domain:   ifq.Query.FQDN,
		Question: ifq.Query.QType,
		RCode:    reply.Rcode,
		Answer:   reply.Answer,
		Ns:       reply.Ns,
		Extra:    reply.Extra,
		Resolver: ifq.Resolver.Info.Copy(),
	}
}

// NewTCPResolver returns a new TPCResolver.
func NewTCPResolver(resolver *Resolver) *TCPResolver {
	var instanceID uint32
	return &TCPResolver{
		BasicResolverConn: BasicResolverConn{
			resolver: resolver,
		},
		clientTTL: defaultClientTTL,
		dnsClient: &dns.Client{
			Net:          "tcp",
			Timeout:      defaultConnectTimeout,
			WriteTimeout: tcpWriteTimeout,
		},
		clientStarted:   abool.New(),
		clientHeartbeat: make(chan struct{}),
		stopClient:      func() {},
		connInstanceID:  &instanceID,
		queries:         make(chan *dns.Msg, 1000),
		inFlightQueries: make(map[uint16]*InFlightQuery),
	}
}

// UseTLS enabled TLS for the TCPResolver. TLS settings must be correctly configured in the Resolver.
func (tr *TCPResolver) UseTLS() *TCPResolver {
	tr.dnsClient.Net = "tcp-tls"
	tr.dnsClient.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: tr.resolver.VerifyDomain,
		// TODO: use portbase rng
	}
	return tr
}

func (tr *TCPResolver) submitQuery(_ context.Context, q *Query) *InFlightQuery {
	// make sure client is started
	tr.startClient()

	// create msg
	msg := &dns.Msg{}
	msg.SetQuestion(q.FQDN, uint16(q.QType))

	// save to waitlist
	inFlight := &InFlightQuery{
		Query:          q,
		Msg:            msg,
		Response:       make(chan *dns.Msg),
		Resolver:       tr.resolver,
		Started:        time.Now().UTC(),
		ConnInstanceID: atomic.LoadUint32(tr.connInstanceID),
	}
	tr.Lock()
	// check for existing query
	tr.ensureUniqueID(msg)
	// add query to in flight registry
	tr.inFlightQueries[msg.Id] = inFlight
	tr.Unlock()

	// submit msg for writing
	select {
	case tr.queries <- msg:
	case <-time.After(defaultRequestTimeout):
		return nil
	}

	return inFlight
}

// ensureUniqueID makes sure that ID assigned to msg is unique. TCPResolver must be locked.
func (tr *TCPResolver) ensureUniqueID(msg *dns.Msg) {
	// try a random ID 10000 times
	for i := 0; i < 10000; i++ { // don't try forever
		_, exists := tr.inFlightQueries[msg.Id]
		if !exists {
			return // we are unique, yay!
		}
		msg.Id = dns.Id() // regenerate ID
	}
	// go through the complete space
	var id uint16
	for ; id <= (1<<16)-1; id++ { // don't try forever
		_, exists := tr.inFlightQueries[id]
		if !exists {
			msg.Id = id
			return // we are unique, yay!
		}
	}
}

// Query executes the given query against the resolver.
func (tr *TCPResolver) Query(ctx context.Context, q *Query) (*RRCache, error) {
	// submit to client
	inFlight := tr.submitQuery(ctx, q)
	if inFlight == nil {
		tr.checkClientStatus()
		return nil, ErrTimeout
	}

	var reply *dns.Msg
	select {
	case reply = <-inFlight.Response:
	case <-time.After(defaultRequestTimeout):
		tr.checkClientStatus()
		return nil, ErrTimeout
	}

	if reply == nil {
		// Resolver is shutting down, could be server failure or we are offline
		return nil, ErrFailure
	}

	if tr.resolver.IsBlockedUpstream(reply) {
		return nil, &BlockedUpstreamError{tr.resolver.Info.DescriptiveName()}
	}

	return inFlight.MakeCacheRecord(reply), nil
}

func (tr *TCPResolver) checkClientStatus() {
	// Get client cancel function before waiting in order to not immediately
	// cancel a new client.
	tr.Lock()
	stopClient := tr.stopClient
	tr.Unlock()

	// Check if the client is alive with the heartbeat, if not shut it down.
	select {
	case tr.clientHeartbeat <- struct{}{}:
	case <-time.After(heartbeatTimeout):
		log.Warningf("resolver: heartbeat failed for %s dns client, stopping", tr.resolver.Info.DescriptiveName())
		stopClient()
	}
}

type tcpResolverConnMgr struct {
	tr        *TCPResolver
	responses chan *dns.Msg
	failCnt   int
}

func (tr *TCPResolver) startClient() {
	if tr.clientStarted.SetToIf(false, true) {
		mgr := &tcpResolverConnMgr{
			tr:        tr,
			responses: make(chan *dns.Msg, 100),
		}
		module.StartServiceWorker("dns client", 10*time.Millisecond, mgr.run)
	}
}

func (mgr *tcpResolverConnMgr) run(workerCtx context.Context) error {
	defer mgr.shutdown()
	mgr.tr.clientStarted.Set()

	// Create additional cancel function for this worker.
	clientCtx, stopClient := context.WithCancel(workerCtx)
	mgr.tr.Lock()
	mgr.tr.stopClient = stopClient
	mgr.tr.Unlock()

	// connection lifecycle loop
	for {
		// check if we are shutting down
		select {
		case <-clientCtx.Done():
			return nil
		default:
		}

		// check if we are failing
		if mgr.failCnt >= FailThreshold || mgr.tr.IsFailing() {
			return nil
		}

		// wait for work before creating connection
		proceed := mgr.waitForWork(clientCtx)
		if !proceed {
			return nil
		}

		// create connection
		conn, connClosing, connCtx, cancelConnCtx := mgr.establishConnection()
		if conn == nil {
			mgr.failCnt++
			continue
		}

		// hint network environment at successful connection
		netenv.ReportSuccessfulConnection()

		// handle queries
		proceed = mgr.queryHandler(clientCtx, conn, connClosing, connCtx, cancelConnCtx)
		if !proceed {
			return nil
		}
	}
}

func (mgr *tcpResolverConnMgr) shutdown() {
	// reply to all waiting queries
	mgr.tr.Lock()
	defer mgr.tr.Unlock()

	mgr.tr.clientStarted.UnSet()               // in lock to guarantee to set before submitQuery proceeds
	atomic.AddUint32(mgr.tr.connInstanceID, 1) // increase instance counter

	for id, inFlight := range mgr.tr.inFlightQueries {
		close(inFlight.Response)
		delete(mgr.tr.inFlightQueries, id)
	}

	// hint network environment at failed connection
	if mgr.failCnt >= FailThreshold {
		netenv.ReportFailedConnection()
	}
}

func (mgr *tcpResolverConnMgr) waitForWork(clientCtx context.Context) (proceed bool) {
	// wait until there is something to do
	mgr.tr.Lock()
	waiting := len(mgr.tr.inFlightQueries)
	mgr.tr.Unlock()
	if waiting > 0 {
		// queue abandoned queries
		ignoreBefore := time.Now().Add(-ignoreQueriesAfter)
		currentConnInstanceID := atomic.LoadUint32(mgr.tr.connInstanceID)
		mgr.tr.Lock()
		defer mgr.tr.Unlock()
		for id, inFlight := range mgr.tr.inFlightQueries {
			if inFlight.Started.Before(ignoreBefore) {
				// remove old queries
				close(inFlight.Response)
				delete(mgr.tr.inFlightQueries, id)
			} else if inFlight.ConnInstanceID != currentConnInstanceID {
				inFlight.ConnInstanceID = currentConnInstanceID
				// re-inject queries that died with a previously failed connection
				select {
				case mgr.tr.queries <- inFlight.Msg:
				default:
					log.Warningf("resolver: failed to re-inject abandoned query to %s", mgr.tr.resolver.Info.DescriptiveName())
				}
			}
			// in-flight queries that match the connection instance ID are not changed. They are already in the queue.
		}
		return true
	}

	// wait for first query
	select {
	case <-clientCtx.Done():
		return false
	case msg := <-mgr.tr.queries:
		// re-insert query, we will handle it later
		module.StartWorker("reinject triggering dns query", func(ctx context.Context) error {
			select {
			case mgr.tr.queries <- msg:
			case <-time.After(2 * time.Second):
				log.Warningf("resolver: failed to re-inject waking query to %s", mgr.tr.resolver.Info.DescriptiveName())
			}
			return nil
		})
	}

	return true
}

func (mgr *tcpResolverConnMgr) establishConnection() (
	conn *dns.Conn,
	connClosing *abool.AtomicBool,
	connCtx context.Context,
	cancelConnCtx context.CancelFunc,
) {
	// refresh dialer to set an authenticated local address
	// TODO: lock dnsClient (only manager should run at any time, so this should not be an issue)
	mgr.tr.dnsClient.Dialer = &net.Dialer{
		LocalAddr: getLocalAddr("tcp"),
		Timeout:   defaultConnectTimeout,
		KeepAlive: defaultClientTTL,
	}
	// connect
	var err error
	conn, err = mgr.tr.dnsClient.Dial(mgr.tr.resolver.ServerAddress)
	if err != nil {
		log.Debugf("resolver: failed to connect to %s", mgr.tr.resolver.Info.DescriptiveName())
		return nil, nil, nil, nil
	}
	connCtx, cancelConnCtx = context.WithCancel(context.Background())
	connClosing = abool.New()

	// Get amount of in waiting queries.
	mgr.tr.Lock()
	waitingQueries := len(mgr.tr.inFlightQueries)
	mgr.tr.Unlock()

	// Log that a connection to the resolver was established.
	log.Debugf(
		"resolver: connected to %s with %d queries waiting",
		mgr.tr.resolver.Info.DescriptiveName(),
		waitingQueries,
	)

	// start reader
	module.StartServiceWorker("dns client reader", 10*time.Millisecond, func(clientCtx context.Context) error {
		return mgr.msgReader(conn, connClosing, cancelConnCtx)
	})

	return conn, connClosing, connCtx, cancelConnCtx
}

func (mgr *tcpResolverConnMgr) queryHandler( //nolint:golint // context.Context _is_ the first parameter.
	clientCtx context.Context,
	conn *dns.Conn,
	connClosing *abool.AtomicBool,
	connCtx context.Context,
	cancelConnCtx context.CancelFunc,
) (proceed bool) {
	var readyToRecycle bool
	ttlTimer := time.After(mgr.tr.clientTTL)

	// clean up connection
	defer func() {
		connClosing.Set() // silence connection errors
		cancelConnCtx()
		_ = conn.Close()

		// increase instance counter
		atomic.AddUint32(mgr.tr.connInstanceID, 1)
	}()

	for {
		select {
		case <-mgr.tr.clientHeartbeat:
			// respond to alive checks

		case <-clientCtx.Done():
			// module shutdown
			return false

		case <-connCtx.Done():
			// connection error
			return true

		case <-ttlTimer:
			// connection TTL reached, rebuild connection
			// but handle all in flight queries first
			readyToRecycle = true
			// trigger check
			select {
			case mgr.responses <- nil:
			default:
				// queue is full, check will be triggered anyway
			}

		case msg := <-mgr.tr.queries:
			// write query
			_ = conn.SetWriteDeadline(time.Now().Add(mgr.tr.dnsClient.WriteTimeout))
			err := conn.WriteMsg(msg)
			if err != nil {
				mgr.logConnectionError(err, conn, connClosing, false)
				return true
			}

		case msg := <-mgr.responses:
			if msg != nil {
				mgr.handleQueryResponse(conn, msg)
			}

			if readyToRecycle {
				// check to see if we can recycle the connection
				mgr.tr.Lock()
				activeQueries := len(mgr.tr.inFlightQueries)
				mgr.tr.Unlock()
				if activeQueries == 0 {
					log.Debugf("resolver: recycling conn to %s", mgr.tr.resolver.Info.DescriptiveName())
					return true
				}
			}

		}
	}
}

func (mgr *tcpResolverConnMgr) handleQueryResponse(conn *dns.Conn, msg *dns.Msg) {
	// handle query from resolver
	mgr.tr.Lock()
	inFlight, ok := mgr.tr.inFlightQueries[msg.Id]
	if ok {
		delete(mgr.tr.inFlightQueries, msg.Id)
	}
	mgr.tr.Unlock()

	if !ok {
		log.Debugf(
			"resolver: received possibly unsolicited reply from %s: txid=%d q=%+v",
			mgr.tr.resolver.Info.DescriptiveName(),
			msg.Id,
			msg.Question,
		)
		return
	}

	select {
	case inFlight.Response <- msg:
		mgr.failCnt = 0 // reset fail counter
		// responded!
		return
	default:
		// no one is listening for that response.
	}

	// if caching is disabled we're done
	if inFlight.Query.NoCaching {
		return
	}

	// persist to database
	rrCache := inFlight.MakeCacheRecord(msg)
	rrCache.Clean(minTTL)
	err := rrCache.Save()
	if err != nil {
		log.Warningf(
			"resolver: failed to cache RR for %s%s: %s",
			inFlight.Query.FQDN,
			inFlight.Query.QType.String(),
			err,
		)
	}
}

func (mgr *tcpResolverConnMgr) msgReader(
	conn *dns.Conn,
	connClosing *abool.AtomicBool,
	cancelConnCtx context.CancelFunc,
) error {
	defer cancelConnCtx()
	for {
		msg, err := conn.ReadMsg()
		if err != nil {
			mgr.logConnectionError(err, conn, connClosing, true)
			return nil
		}
		mgr.responses <- msg
	}
}

func (mgr *tcpResolverConnMgr) logConnectionError(err error, conn *dns.Conn, connClosing *abool.AtomicBool, reading bool) {
	// Check if we are the first to see an error.
	if connClosing.SetToIf(false, true) {
		// Get amount of in flight queries.
		mgr.tr.Lock()
		inFlightQueries := len(mgr.tr.inFlightQueries)
		mgr.tr.Unlock()

		// Log error.
		switch {
		case errors.Is(err, io.EOF):
			log.Debugf(
				"resolver: connection to %s was closed with %d in-flight queries",
				mgr.tr.resolver.Info.DescriptiveName(),
				inFlightQueries,
			)
		case reading:
			log.Warningf(
				"resolver: read error from %s with %d in-flight queries: %s",
				mgr.tr.resolver.Info.DescriptiveName(),
				inFlightQueries,
				err,
			)
		default:
			log.Warningf(
				"resolver: write error to %s with %d in-flight queries: %s",
				mgr.tr.resolver.Info.DescriptiveName(),
				inFlightQueries,
				err,
			)
		}
	}
}
