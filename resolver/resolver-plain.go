package resolver

import (
	"context"
	"net"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
)

var (
	defaultClientTTL      = 5 * time.Minute
	defaultRequestTimeout = 3 * time.Second // dns query
	defaultConnectTimeout = 5 * time.Second // tcp/tls
	maxRequestTimeout     = 5 * time.Second
)

// PlainResolver is a resolver using plain DNS.
type PlainResolver struct {
	BasicResolverConn
}

// NewPlainResolver returns a new TPCResolver.
func NewPlainResolver(resolver *Resolver) *PlainResolver {
	return &PlainResolver{
		BasicResolverConn: BasicResolverConn{
			resolver: resolver,
		},
	}
}

// Query executes the given query against the resolver.
func (pr *PlainResolver) Query(ctx context.Context, q *Query) (*RRCache, error) {
	// create query
	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(q.FQDN, uint16(q.QType))

	// get timeout from context and config
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); !ok {
		timeout = 0
	} else {
		timeout = time.Until(deadline)
	}
	if timeout > defaultRequestTimeout {
		timeout = defaultRequestTimeout
	}

	// create client
	dnsClient := &dns.Client{
		Timeout: timeout,
		Dialer: &net.Dialer{
			Timeout:   timeout,
			LocalAddr: getLocalAddr("udp"),
		},
	}

	// query server
	reply, ttl, err := dnsClient.Exchange(dnsQuery, pr.resolver.ServerAddress)
	log.Tracer(ctx).Tracef("resolver: query took %s", ttl)
	// error handling
	if err != nil {
		// Hint network environment at failed connection if err is not a timeout.
		if nErr, ok := err.(net.Error); ok && !nErr.Timeout() {
			netenv.ReportFailedConnection()
		}

		return nil, err
	}

	// check if blocked
	if pr.resolver.IsBlockedUpstream(reply) {
		return nil, &BlockedUpstreamError{pr.resolver.GetName()}
	}

	// hint network environment at successful connection
	netenv.ReportSuccessfulConnection()

	newRecord := &RRCache{
		Domain:      q.FQDN,
		Question:    q.QType,
		Answer:      reply.Answer,
		Ns:          reply.Ns,
		Extra:       reply.Extra,
		Server:      pr.resolver.Server,
		ServerScope: pr.resolver.ServerIPScope,
	}

	// TODO: check if reply.Answer is valid
	return newRecord, nil
}
