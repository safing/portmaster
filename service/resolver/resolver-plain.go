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

	// plainRetransmitInterval is the time after which a plain UDP query is
	// sent again if no reply arrived, as stub resolvers do (RFC 1035 4.2.1).
	// A single lost packet would otherwise fail the query after the full
	// request timeout.
	plainRetransmitInterval = 1 * time.Second
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
	// The OS may hand out the port of a recently closed socket, whose query to
	// the same server was redirected to the Portmaster nameserver. Its
	// conntrack entry would redirect this query back to Portmaster itself.
	if remoteAddr, ok := rawConn.RemoteAddr().(*net.UDPAddr); ok {
		clearUDPFlow(localAddr, remoteAddr)
	}
	// The first packet consumes the pre-authentication. Release it afterwards
	// in case no packet was sent, which is a no-op otherwise.
	defer releaseLocalPort(networkUDP, localPort, false)

	return exchangeWithRetransmit(ctx, conn, dnsQuery, dnsClient.Timeout, plainRetransmitInterval)
}

// exchangeWithRetransmit sends query over the UDP conn and waits for the
// reply. If none arrives within retransmitAfter, the query is sent again on
// the same socket, until timeout (and the context deadline) is reached.
// Replies to an earlier transmission are equally valid, as they have the same
// ID. A zero timeout means the default request timeout.
func exchangeWithRetransmit(ctx context.Context, conn *dns.Conn, query *dns.Msg, timeout, retransmitAfter time.Duration) (*dns.Msg, time.Duration, error) {
	started := time.Now()
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	deadline := started.Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	// Abort a pending read when the context is canceled.
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetReadDeadline(time.Now())
	})
	defer stop()

	for {
		err := conn.SetWriteDeadline(deadline)
		if err != nil {
			return nil, time.Since(started), err
		}
		err = conn.WriteMsg(query)
		if err != nil {
			return nil, time.Since(started), err
		}

		readUntil := time.Now().Add(retransmitAfter)
		if readUntil.After(deadline) {
			readUntil = deadline
		}
		reply, err := readReply(conn, query.Id, readUntil)
		switch {
		case err == nil:
			return reply, time.Since(started), nil
		case ctx.Err() != nil:
			return nil, time.Since(started), ctx.Err()
		case !isTimeout(err) || !time.Now().Before(deadline):
			return nil, time.Since(started), err
		}
	}
}

// readReply reads from conn until a message with the given ID arrives or the
// deadline passes. Messages with other IDs (late replies to other queries)
// are skipped.
func readReply(conn *dns.Conn, id uint16, deadline time.Time) (*dns.Msg, error) {
	err := conn.SetReadDeadline(deadline)
	if err != nil {
		return nil, err
	}
	for {
		reply, err := conn.ReadMsg()
		if err != nil {
			return nil, err
		}
		if reply.Id == id {
			return reply, nil
		}
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
