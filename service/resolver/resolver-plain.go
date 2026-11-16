package resolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
)

var (
	defaultClientTTL      = 5 * time.Minute
	defaultRequestTimeout = 3 * time.Second // dns query
	defaultConnectTimeout = 5 * time.Second // tcp/tls
	maxRequestTimeout     = 5 * time.Second
)

var errUnexpectedLocalAddr = errors.New("unexpected local address of udp connection")

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
	}

	// query server
	reply, ttl, err := pr.exchange(ctx, dnsClient, dnsQuery)
	log.Tracer(ctx).Tracef("resolver: query took %s", ttl)
	// error handling
	if err != nil {
		// Nothing left the host if the local port could not be bound. Do not
		// count this against the resolver or the network.
		if netutils.IsLocalBindError(err) {
			log.Tracer(ctx).Debugf("resolver: failed to bind local port for query to %s: %s", pr.resolver.Info.DescriptiveName(), err)
			return nil, fmt.Errorf("%w for query to %s: %w", ErrLocalBind, pr.resolver.Info.DescriptiveName(), err)
		}

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

// exchange sends the query over a fresh UDP socket and returns the reply.
//
// The local port is chosen by the OS, which never hands out a port that is
// in use or reserved (e.g. by Hyper-V excluded port ranges on Windows). The
// port is pre-authenticated after the socket is bound and before the first
// packet leaves, so the firewall attributes the query to Portmaster itself.
func (pr *PlainResolver) exchange(ctx context.Context, dnsClient *dns.Client, dnsQuery *dns.Msg) (*dns.Msg, time.Duration, error) {
	dialer := &net.Dialer{Timeout: dnsClient.Timeout}
	rawConn, err := dialer.DialContext(ctx, networkUDP, pr.resolver.ServerAddress)
	if err != nil {
		return nil, 0, err
	}
	conn := &dns.Conn{Conn: rawConn, UDPSize: dnsClient.UDPSize}
	defer func() { _ = conn.Close() }()

	localAddr, ok := rawConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, 0, fmt.Errorf("%w: %v", errUnexpectedLocalAddr, rawConn.LocalAddr())
	}
	localPort := uint16(localAddr.Port) //nolint:gosec // Ports of net.Addr are within the uint16 range.
	authorizeLocalPort(networkUDP, localPort)
	// The first packet consumes the pre-authentication. Release it afterwards
	// in case no packet was sent, which is a no-op otherwise.
	defer releaseLocalPort(networkUDP, localPort, false)

	return dnsClient.ExchangeWithConnContext(ctx, dnsQuery, conn)
}
