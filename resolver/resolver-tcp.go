package resolver

import (
	"context"
	"crypto/tls"
	"net"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
	"github.com/tevino/abool"
)

const (
	tcpWriteTimeout    = 1 * time.Second
	ignoreQueriesAfter = 10 * time.Minute
)

// TCPResolver is a resolver using just a single tcp connection with pipelining.
type TCPResolver struct {
	BasicResolverConn

	clientTTL time.Duration
	dnsClient *dns.Client

	clientStarted   *abool.AtomicBool
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
		Domain:      ifq.Query.FQDN,
		Question:    ifq.Query.QType,
		Answer:      reply.Answer,
		Ns:          reply.Ns,
		Extra:       reply.Extra,
		Server:      ifq.Resolver.Server,
		ServerScope: ifq.Resolver.ServerIPScope,
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
		connInstanceID:  &instanceID,
		queries:         make(chan *dns.Msg, 100),
		inFlightQueries: make(map[uint16]*InFlightQuery),
		clientStarted:   abool.New(),
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
	for i := 0; i < 10; i++ { // don't try forever
		_, exists := tr.inFlightQueries[msg.Id]
		if !exists {
			break // we are unique, yay!
		}
		msg.Id = dns.Id() // regenerate ID
	}
	// add query to in flight registry
	tr.inFlightQueries[msg.Id] = inFlight
	tr.Unlock()

	// submit msg for writing
	tr.queries <- msg

	return inFlight
}

// Query executes the given query against the resolver.
func (tr *TCPResolver) Query(ctx context.Context, q *Query) (*RRCache, error) {
	// submit to client
	inFlight := tr.submitQuery(ctx, q)
	var reply *dns.Msg

	select {
	case reply = <-inFlight.Response:
	case <-time.After(defaultRequestTimeout):
		tr.ReportFailure()
		return nil, ErrTimeout
	}

	if reply == nil {
		// Resolver is shutting down, could be server failure or we are offline
		return nil, ErrFailure
	}

	if tr.resolver.IsBlockedUpstream(reply) {
		return nil, &BlockedUpstreamError{tr.resolver.GetName()}
	}

	return inFlight.MakeCacheRecord(reply), nil
}

type tcpResolverConnMgr struct {
	tr            *TCPResolver
	workerCtx     context.Context
	conn          *dns.Conn
	connCtx       context.Context
	cancelConnCtx func()
	connTimer     *time.Timer
	connClosing   *abool.AtomicBool
	responses     chan *dns.Msg
	failCnt       int
}

func (tr *TCPResolver) startClient() {
	if tr.clientStarted.SetToIf(false, true) {
		mgr := &tcpResolverConnMgr{
			tr:          tr,
			connTimer:   time.NewTimer(tr.clientTTL),
			connClosing: abool.New(),
			responses:   make(chan *dns.Msg, 100),
		}
		module.StartServiceWorker("dns client", 10*time.Millisecond, mgr.run)
	}
}

func (mgr *tcpResolverConnMgr) run(workerCtx context.Context) error {
	mgr.workerCtx = workerCtx

	// connection lifecycle loop
	for {
		// check if we are failing
		if mgr.failCnt >= FailThreshold || mgr.tr.IsFailing() {
			mgr.shutdown()
			return nil
		}

		// clean up anything that is left over
		mgr.cleanupConnection()

		// wait for work before creating connection
		proceed := mgr.waitForWork()
		if !proceed {
			mgr.shutdown()
			return nil
		}

		// create connection
		success := mgr.establishConnection()
		if !success {
			mgr.failCnt++
			continue
		}

		// hint network environment at successful connection
		netenv.ReportSuccessfulConnection()

		// handle queries
		proceed = mgr.queryHandler()
		if !proceed {
			mgr.shutdown()
			return nil
		}
	}
}

func (mgr *tcpResolverConnMgr) cleanupConnection() {
	// cleanup old connection
	if mgr.conn != nil {
		mgr.connClosing.Set() // silence connection errors
		_ = mgr.conn.Close()
		if mgr.cancelConnCtx != nil {
			mgr.cancelConnCtx()
		}

		// delete old connection
		mgr.conn = nil

		// increase instance counter
		atomic.AddUint32(mgr.tr.connInstanceID, 1)
	}
}

func (mgr *tcpResolverConnMgr) shutdown() {
	mgr.cleanupConnection()

	// reply to all waiting queries
	mgr.tr.Lock()
	for id, inFlight := range mgr.tr.inFlightQueries {
		close(inFlight.Response)
		delete(mgr.tr.inFlightQueries, id)
	}
	mgr.tr.clientStarted.UnSet()               // in lock to guarantee to set before submitQuery proceeds
	atomic.AddUint32(mgr.tr.connInstanceID, 1) // increase instance counter
	mgr.tr.Unlock()

	// hint network environment at failed connection
	if mgr.failCnt >= FailThreshold {
		netenv.ReportFailedConnection()
	}
}

func (mgr *tcpResolverConnMgr) waitForWork() (proceed bool) {
	// wait until there is something to do
	mgr.tr.Lock()
	waiting := len(mgr.tr.inFlightQueries)
	mgr.tr.Unlock()
	if waiting > 0 {
		// queue abandoned queries
		ignoreBefore := time.Now().Add(-ignoreQueriesAfter)
		currentConnInstanceID := atomic.LoadUint32(mgr.tr.connInstanceID)
		mgr.tr.Lock()
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
					log.Warningf("resolver: failed to re-inject abandoned query to %s", mgr.tr.resolver.Name)
				}
			}
			// in-flight queries that match the connection instance ID are not changed. They are already in the queue.
		}
		mgr.tr.Unlock()
	} else {
		// wait for first query
		select {
		case <-mgr.workerCtx.Done():
			return false
		case msg := <-mgr.tr.queries:
			// re-insert query, we will handle it later
			select {
			case mgr.tr.queries <- msg:
			default:
				log.Warningf("resolver: failed to re-inject waking query to %s", mgr.tr.resolver.Name)
			}
		}
	}

	return true
}

func (mgr *tcpResolverConnMgr) establishConnection() (success bool) {
	// create connection
	mgr.connCtx, mgr.cancelConnCtx = context.WithCancel(mgr.workerCtx)
	mgr.connClosing = abool.New()
	// refresh dialer to set an authenticated local address
	// TODO: lock dnsClient (only manager should run at any time, so this should not be an issue)
	mgr.tr.dnsClient.Dialer = &net.Dialer{
		LocalAddr: getLocalAddr("tcp"),
		Timeout:   defaultConnectTimeout,
		KeepAlive: defaultClientTTL,
	}
	// connect
	c, err := mgr.tr.dnsClient.Dial(mgr.tr.resolver.ServerAddress)
	if err != nil {
		log.Debugf("resolver: failed to connect to %s (%s)", mgr.tr.resolver.Name, mgr.tr.resolver.ServerAddress)
		return false
	}
	mgr.conn = c
	log.Debugf("resolver: connected to %s (%s)", mgr.tr.resolver.Name, mgr.conn.RemoteAddr())

	// reset timer
	mgr.connTimer.Stop()
	select {
	case <-mgr.connTimer.C: // try to empty the timer
	default:
	}
	mgr.connTimer.Reset(mgr.tr.clientTTL)

	// start reader
	module.StartServiceWorker("dns client reader", 10*time.Millisecond, mgr.msgReader)

	return true
}

func (mgr *tcpResolverConnMgr) queryHandler() (proceed bool) { //nolint:gocognit
	var readyToRecycle bool

	for {
		select {
		case <-mgr.workerCtx.Done():
			// module shutdown
			return false

		case <-mgr.connCtx.Done():
			// connection error
			return true

		case <-mgr.connTimer.C:
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
			_ = mgr.conn.SetWriteDeadline(time.Now().Add(mgr.tr.dnsClient.WriteTimeout))
			err := mgr.conn.WriteMsg(msg)
			if err != nil {
				if mgr.connClosing.SetToIf(false, true) {
					mgr.cancelConnCtx()
					log.Warningf("resolver: write error to %s (%s): %s", mgr.tr.resolver.Name, mgr.conn.RemoteAddr(), err)
				}
				return true
			}

		case msg := <-mgr.responses:
			if msg != nil { // nil messages only trigger the recycle check
				// handle query from resolver
				mgr.tr.Lock()
				inFlight, ok := mgr.tr.inFlightQueries[msg.Id]
				if ok {
					delete(mgr.tr.inFlightQueries, msg.Id)
				}
				mgr.tr.Unlock()

				if ok {
					select {
					case inFlight.Response <- msg:
						mgr.failCnt = 0 // reset fail counter
						// responded!
					default:
						// save to cache, if enabled
						if !inFlight.Query.NoCaching {
							// persist to database
							rrCache := inFlight.MakeCacheRecord(msg)
							rrCache.Clean(600)
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
					}
				} else {
					log.Debugf(
						"resolver: received possibly unsolicited reply from %s (%s): txid=%d q=%+v",
						mgr.tr.resolver.Name,
						mgr.conn.RemoteAddr(),
						msg.Id,
						msg.Question,
					)
				}
			}

			if readyToRecycle {
				// check to see if we can recycle the connection
				mgr.tr.Lock()
				activeQueries := len(mgr.tr.inFlightQueries)
				mgr.tr.Unlock()
				if activeQueries == 0 {
					log.Debugf("resolver: recycling conn to %s (%s)", mgr.tr.resolver.Name, mgr.conn.RemoteAddr())
					return true
				}
			}

		}
	}
}

func (mgr *tcpResolverConnMgr) msgReader(workerCtx context.Context) error {
	// copy values from manager
	conn := mgr.conn
	cancelConnCtx := mgr.cancelConnCtx
	connClosing := mgr.connClosing

	for {
		msg, err := conn.ReadMsg()
		if err != nil {
			if connClosing.SetToIf(false, true) {
				cancelConnCtx()
				log.Warningf("resolver: read error from %s (%s): %s", mgr.tr.resolver.Name, mgr.conn.RemoteAddr(), err)
			}
			return nil
		}
		mgr.responses <- msg
	}
}
