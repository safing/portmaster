package resolver

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
)

// DNS Resolver Attributes
const (
	ServerTypeDNS = "dns"
	ServerTypeTCP = "tcp"
	ServerTypeDoT = "dot"
	ServerTypeDoH = "doh"

	ServerSourceConfigured = "config"
	ServerSourceAssigned   = "dhcp"
	ServerSourceMDNS       = "mdns"
)

var (
	// FailThreshold is amount of errors a resolvers must experience in order to be regarded as failed.
	FailThreshold = 5
)

// Resolver holds information about an active resolver.
type Resolver struct {
	// Server config url (and ID)
	Server string

	// Name is the name of the resolver as passed via
	// ?name=.
	Name string

	// UpstreamBlockDetection defines the detection type
	// to identifier upstream DNS query blocking.
	// Valid values are:
	//	 - zeroip
	//	 - empty
	//   - refused (default)
	//	 - disabled
	UpstreamBlockDetection string

	// Parsed config
	ServerType    string
	ServerAddress string
	ServerIP      net.IP
	ServerIPScope int8
	ServerPort    uint16

	// Special Options
	VerifyDomain string
	Search       []string
	SkipFQDN     string

	Source string

	// logic interface
	Conn ResolverConn
}

// IsBlockedUpstream returns true if the request has been blocked
// upstream.
func (resolver *Resolver) IsBlockedUpstream(answer *dns.Msg) bool {
	return isBlockedUpstream(resolver, answer)
}

// GetName returns the name of the server. If no name
// is configured the server address is returned.
func (resolver *Resolver) GetName() string {
	if resolver.Name != "" {
		return resolver.Name
	}

	return resolver.Server
}

// String returns the URL representation of the resolver.
func (resolver *Resolver) String() string {
	return resolver.GetName()
}

// ResolverConn is an interface to implement different types of query backends.
type ResolverConn interface { //nolint:go-lint // TODO
	Query(ctx context.Context, q *Query) (*RRCache, error)
	ReportFailure()
	IsFailing() bool
}

// BasicResolverConn implements ResolverConn for standard dns clients.
type BasicResolverConn struct {
	sync.Mutex // for lastFail

	resolver      *Resolver
	clientManager *dnsClientManager

	lastFail time.Time
	fails    int
}

// ReportFailure reports that an error occurred with this resolver.
func (brc *BasicResolverConn) ReportFailure() {
	if !netenv.Online() {
		// don't mark failed if we are offline
		return
	}

	brc.Lock()
	defer brc.Unlock()
	now := time.Now().UTC()
	failDuration := time.Duration(nameserverRetryRate()) * time.Second

	// reset fail counter if currently not failing
	if now.Add(-failDuration).After(brc.lastFail) {
		brc.fails = 0
	}

	// update
	brc.lastFail = now
	brc.fails++
}

// IsFailing returns if this resolver is currently failing.
func (brc *BasicResolverConn) IsFailing() bool {
	brc.Lock()
	defer brc.Unlock()

	failDuration := time.Duration(nameserverRetryRate()) * time.Second
	return brc.fails >= FailThreshold && time.Now().UTC().Add(-failDuration).Before(brc.lastFail)
}

// Query executes the given query against the resolver.
func (brc *BasicResolverConn) Query(ctx context.Context, q *Query) (*RRCache, error) {
	// convenience
	resolver := brc.resolver

	// create query
	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(q.FQDN, uint16(q.QType))

	// start
	var reply *dns.Msg
	var ttl time.Duration
	var err error
	var conn *dns.Conn
	var new bool
	var tries int

	for ; tries < 3; tries++ {

		// first get connection
		dc := brc.clientManager.getDNSClient()
		conn, new, err = dc.getConn()
		if err != nil {
			log.Tracer(ctx).Tracef("resolver: failed to connect to %s: %s", resolver.Server, err)
			// remove client from pool
			dc.destroy()
			// report that resolver had an error
			brc.ReportFailure()
			// hint network environment at failed connection
			netenv.ReportFailedConnection()

			// TODO: handle special cases
			// 1. connect: network is unreachable
			// 2. timeout

			// try again
			continue
		}
		if new {
			log.Tracer(ctx).Tracef("resolver: created new connection to %s (%s)", resolver.Name, resolver.ServerAddress)
		} else {
			log.Tracer(ctx).Tracef("resolver: reusing connection to %s (%s)", resolver.Name, resolver.ServerAddress)
		}

		// query server
		reply, ttl, err = dc.client.ExchangeWithConn(dnsQuery, conn)
		log.Tracer(ctx).Tracef("resolver: query took %s", ttl)

		// error handling
		if err != nil {
			log.Tracer(ctx).Tracef("resolver: query to %s encountered error: %s", resolver.Server, err)

			// remove client from pool
			dc.destroy()

			// temporary error
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Tracer(ctx).Tracef("resolver: retrying to resolve %s%s with %s, error is temporary", q.FQDN, q.QType, resolver.Server)
				// try again
				continue
			}

			// report failed if dns (nothing happens at getConn())
			if resolver.ServerType == ServerTypeDNS {
				// report that resolver had an error
				brc.ReportFailure()
				// hint network environment at failed connection
				netenv.ReportFailedConnection()
			}

			// permanent error
			break
		} else if reply == nil {
			// remove client from pool
			dc.destroy()

			log.Errorf("resolver: successful query for %s%s to %s, but reply was nil", q.FQDN, q.QType, resolver.Server)
			return nil, errors.New("internal error")
		}

		// make client available (again)
		dc.addToPool()

		if resolver.IsBlockedUpstream(reply) {
			return nil, &BlockedUpstreamError{resolver.GetName()}
		}

		// no error
		break
	}

	if err != nil {
		return nil, err
		// TODO: mark as failed
	} else if reply == nil {
		log.Errorf("resolver: queried %s for %s%s (%d tries), but reply was nil", q.FQDN, q.QType, resolver.GetName(), tries+1)
		return nil, errors.New("internal error")
	}

	// hint network environment at successful connection
	netenv.ReportSuccessfulConnection()

	newRecord := &RRCache{
		Domain:      q.FQDN,
		Question:    q.QType,
		Answer:      reply.Answer,
		Ns:          reply.Ns,
		Extra:       reply.Extra,
		Server:      resolver.Server,
		ServerScope: resolver.ServerIPScope,
	}

	// TODO: check if reply.Answer is valid
	return newRecord, nil
}
