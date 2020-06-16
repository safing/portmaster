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

	clientTTL      time.Duration
	dnsClient      *dns.Client
	dnsConnection  *dns.Conn
	connInstanceID *uint32

	queries         chan *dns.Msg
	inFlightQueries map[uint16]*InFlightQuery
	clientStarted   *abool.AtomicBool
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

func (tr *TCPResolver) client(workerCtx context.Context) error { //nolint:gocognit,gocyclo // TODO
	connTimer := time.NewTimer(tr.clientTTL)
	connClosing := abool.New()
	var connCtx context.Context
	var cancelConnCtx func()
	var recycleConn bool
	var shuttingDown bool
	var incoming = make(chan *dns.Msg, 100)

	// enable client restarting after crash
	defer tr.clientStarted.UnSet()

connMgmt:
	for {
		// cleanup old connection
		if tr.dnsConnection != nil {
			connClosing.Set()
			_ = tr.dnsConnection.Close()
			cancelConnCtx()

			tr.dnsConnection = nil
			atomic.AddUint32(tr.connInstanceID, 1)
		}

		// check if we are shutting down or failing
		if shuttingDown || tr.IsFailing() {
			// reply to all waiting queries
			tr.Lock()
			for id, inFlight := range tr.inFlightQueries {
				close(inFlight.Response)
				delete(tr.inFlightQueries, id)
			}
			tr.clientStarted.UnSet() // in lock to guarantee to set before submitQuery proceeds
			tr.Unlock()

			// hint network environment at failed connection
			netenv.ReportFailedConnection()

			cancelConnCtx() // The linter said so. Don't even...
			return nil
		}

		// wait until there is something to do
		tr.Lock()
		waiting := len(tr.inFlightQueries)
		tr.Unlock()
		if waiting > 0 {
			// queue abandoned queries
			ignoreBefore := time.Now().Add(-ignoreQueriesAfter)
			currentConnInstanceID := atomic.LoadUint32(tr.connInstanceID)
			tr.Lock()
			for id, inFlight := range tr.inFlightQueries {
				if inFlight.Started.Before(ignoreBefore) {
					// remove
					delete(tr.inFlightQueries, id)
				} else if inFlight.ConnInstanceID != currentConnInstanceID {
					inFlight.ConnInstanceID = currentConnInstanceID
					// re-inject
					select {
					case tr.queries <- inFlight.Msg:
					default:
						log.Warningf("resolver: failed to re-inject abandoned query to %s", tr.resolver.Name)
					}
				}
			}
			tr.Unlock()
		} else {
			// wait for first query
			select {
			case <-workerCtx.Done():
				// abort
				shuttingDown = true
				continue connMgmt
			case msg := <-tr.queries:
				// re-insert, we will handle later
				select {
				case tr.queries <- msg:
				default:
					log.Warningf("resolver: failed to re-inject waking query to %s", tr.resolver.Name)
				}
			}
		}

		// create connection
		connCtx, cancelConnCtx = context.WithCancel(workerCtx)
		// refresh dialer for authenticated local address
		tr.dnsClient.Dialer = &net.Dialer{
			LocalAddr: getLocalAddr("tcp"),
			Timeout:   defaultConnectTimeout,
			KeepAlive: defaultClientTTL,
		}
		// connect
		c, err := tr.dnsClient.Dial(tr.resolver.ServerAddress)
		if err != nil {
			tr.ReportFailure()
			log.Debugf("resolver: failed to connect to %s (%s)", tr.resolver.Name, tr.resolver.ServerAddress)
			continue connMgmt
		}
		tr.dnsConnection = c
		log.Debugf("resolver: connected to %s (%s)", tr.resolver.Name, tr.dnsConnection.RemoteAddr())

		// hint network environment at successful connection
		netenv.ReportSuccessfulConnection()

		// reset timer
		connTimer.Stop()
		select {
		case <-connTimer.C: // try to empty the timer
		default:
		}
		connTimer.Reset(tr.clientTTL)
		recycleConn = false

		// start reader
		module.StartWorker("dns client reader", func(ctx context.Context) error {
			conn := tr.dnsConnection
			for {
				msg, err := conn.ReadMsg()
				if err != nil {
					if connClosing.SetToIf(false, true) {
						cancelConnCtx()
						tr.ReportFailure()
						log.Warningf("resolver: read error from %s (%s): %s", tr.resolver.Name, tr.dnsConnection.RemoteAddr(), err)
					}
					return nil
				}
				incoming <- msg
			}
		})

		// query management
		for {
			select {
			case <-workerCtx.Done():
				// shutting down
				shuttingDown = true
				continue connMgmt
			case <-connCtx.Done():
				// connection error
				continue connMgmt
			case <-connTimer.C:
				// client TTL expired, recycle connection
				recycleConn = true
				// trigger check
				select {
				case incoming <- nil:
				default:
					// quere is full anyway, do nothing
				}

			case msg := <-tr.queries:
				// write query
				_ = tr.dnsConnection.SetWriteDeadline(time.Now().Add(tr.dnsClient.WriteTimeout))
				err := tr.dnsConnection.WriteMsg(msg)
				if err != nil {
					if connClosing.SetToIf(false, true) {
						cancelConnCtx()
						tr.ReportFailure()
						log.Warningf("resolver: write error to %s (%s): %s", tr.resolver.Name, tr.dnsConnection.RemoteAddr(), err)
					}
					continue connMgmt
				}

			case msg := <-incoming:

				if msg != nil {
					// handle query from resolver
					tr.Lock()
					inFlight, ok := tr.inFlightQueries[msg.Id]
					if ok {
						delete(tr.inFlightQueries, msg.Id)
					}
					tr.Unlock()

					if ok {
						select {
						case inFlight.Response <- msg:
							// responded!
						default:
							// save to cache, if enabled
							if !inFlight.Query.NoCaching {
								// persist to database
								rrCache := inFlight.MakeCacheRecord(msg)
								rrCache.Clean(600)
								err = rrCache.Save()
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
							tr.resolver.Name,
							tr.dnsConnection.RemoteAddr(),
							msg.Id,
							msg.Question,
						)
					}
				}

				// check if we have finished all queries and want to recycle conn
				if recycleConn {
					tr.Lock()
					activeQueries := len(tr.inFlightQueries)
					tr.Unlock()
					if activeQueries == 0 {
						log.Debugf("resolver: recycling conn to %s (%s)", tr.resolver.Name, tr.dnsConnection.RemoteAddr())
						continue connMgmt
					}
				}

			}
		}

	}
}

func (tr *TCPResolver) submitQuery(_ context.Context, q *Query) *InFlightQuery {
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
	tr.inFlightQueries[msg.Id] = inFlight
	tr.Unlock()

	// submit msg for writing
	tr.queries <- msg

	// make sure client is started
	if tr.clientStarted.SetToIf(false, true) {
		module.StartWorker("dns client", tr.client)
	}

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

	if tr.resolver.IsBlockedUpstream(reply) {
		return nil, &BlockedUpstreamError{tr.resolver.GetName()}
	}

	return inFlight.MakeCacheRecord(reply), nil
}
