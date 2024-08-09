package resolver

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
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
	newResolver := &PlainResolver{
		BasicResolverConn: BasicResolverConn{
			resolver: resolver,
		},
	}
	newResolver.BasicResolverConn.init()
	return newResolver
}

// Query executes the given query against the resolver.
func (pr *PlainResolver) Query(ctx context.Context, q *Query) (*RRCache, error) {
	queryStarted := time.Now()

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
		UDPSize: 1024,
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
		var nErr net.Error
		if errors.As(err, &nErr) && !nErr.Timeout() {
			netenv.ReportFailedConnection()
		}

		return nil, err
	}

	// check if blocked
	if pr.resolver.IsBlockedUpstream(reply) {
		return nil, &BlockedUpstreamError{pr.resolver.Info.DescriptiveName()}
	}

	// Hint network environment at successful connection.
	netenv.ReportSuccessfulConnection()

	// Report request duration for metrics.
	reportRequestDuration(queryStarted, pr.resolver)

	newRecord := &RRCache{
		Domain:   q.FQDN,
		Question: q.QType,
		RCode:    reply.Rcode,
		Answer:   reply.Answer,
		Ns:       reply.Ns,
		Extra:    reply.Extra,
		Resolver: pr.resolver.Info.Copy(),
	}

	// TODO: check if reply.Answer is valid
	return newRecord, nil
}

// ForceReconnect forces the resolver to re-establish the connection to the server.
// Does nothing for PlainResolver, as every request uses its own connection.
func (pr *PlainResolver) ForceReconnect(_ context.Context) {}
