package resolver

import (
	"context"
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
	MarkFailed()
	LastFail() time.Time
}

// BasicResolverConn implements ResolverConn for standard dns clients.
type BasicResolverConn struct {
	sync.Mutex // for lastFail

	resolver      *Resolver
	clientManager *clientManager
	lastFail      time.Time
}

// MarkFailed marks the resolver as failed.
func (brc *BasicResolverConn) MarkFailed() {
	if !netenv.Online() {
		// don't mark failed if we are offline
		return
	}

	brc.Lock()
	defer brc.Unlock()
	brc.lastFail = time.Now()
}

// LastFail returns the internal lastfail value while locking the Resolver.
func (brc *BasicResolverConn) LastFail() time.Time {
	brc.Lock()
	defer brc.Unlock()
	return brc.lastFail
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
	var err error
	for i := 0; i < 3; i++ {

		// log query time
		// qStart := time.Now()
		reply, _, err = brc.clientManager.getDNSClient().Exchange(dnsQuery, resolver.ServerAddress)
		// log.Tracef("resolver: query to %s took %s", resolver.Server, time.Now().Sub(qStart))

		// error handling
		if err != nil {
			log.Tracer(ctx).Tracef("resolver: query to %s encountered error: %s", resolver.Server, err)

			// TODO: handle special cases
			// 1. connect: network is unreachable
			// 2. timeout

			// hint network environment at failed connection
			netenv.ReportFailedConnection()

			// temporary error
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Tracer(ctx).Tracef("resolver: retrying to resolve %s%s with %s, error is temporary", q.FQDN, q.QType, resolver.Server)
				continue
			}

			// permanent error
			break
		}

		if resolver.IsBlockedUpstream(reply) {
			return nil, &BlockedUpstreamError{resolver.GetName()}
		}

		// no error
		break
	}

	if err != nil {
		return nil, err
		// TODO: mark as failed
	}

	// hint network environment at successful connection
	netenv.ReportSuccessfulConnection()

	new := &RRCache{
		Domain:      q.FQDN,
		Question:    q.QType,
		Answer:      reply.Answer,
		Ns:          reply.Ns,
		Extra:       reply.Extra,
		Server:      resolver.Server,
		ServerScope: resolver.ServerIPScope,
	}

	// TODO: check if reply.Answer is valid
	return new, nil
}
