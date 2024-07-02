package resolver

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
)

// HTTPSResolver is a resolver using just a single tcp connection with pipelining.
type HTTPSResolver struct {
	BasicResolverConn
	client     *http.Client
	clientLock sync.RWMutex
}

// HTTPSQuery holds the query information for a hTTPSResolverConn.
type HTTPSQuery struct {
	Query    *Query
	Response chan *dns.Msg
}

// MakeCacheRecord creates an RRCache record from a reply.
func (tq *HTTPSQuery) MakeCacheRecord(reply *dns.Msg, resolverInfo *ResolverInfo) *RRCache {
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

// NewHTTPSResolver returns a new HTTPSResolver.
func NewHTTPSResolver(resolver *Resolver) *HTTPSResolver {
	newResolver := &HTTPSResolver{
		BasicResolverConn: BasicResolverConn{
			resolver: resolver,
		},
	}
	newResolver.BasicResolverConn.init()
	newResolver.refreshClient()
	return newResolver
}

// Query executes the given query against the resolver.
func (hr *HTTPSResolver) Query(ctx context.Context, q *Query) (*RRCache, error) {
	queryStarted := time.Now()

	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(q.FQDN, uint16(q.QType))

	// Pack query and convert to base64 string
	buf, err := dnsQuery.Pack()
	if err != nil {
		return nil, err
	}
	b64dns := base64.RawURLEncoding.EncodeToString(buf)

	// Build and execute http request
	url := &url.URL{
		Scheme:     "https",
		Host:       hr.resolver.ServerAddress,
		Path:       hr.resolver.Path,
		ForceQuery: true,
		RawQuery:   fmt.Sprintf("dns=%s", b64dns),
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, err
	}

	// Lock client for usage.
	hr.clientLock.RLock()
	defer hr.clientLock.RUnlock()

	// TODO: Check age of client and force a refresh similar to the TCP resolver.

	resp, err := hr.client.Do(request)
	if err != nil {
		// Hint network environment at failed connection.
		netenv.ReportFailedConnection()

		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http request failed with %s", resp.Status)
	}

	// Try to read the result
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	reply := new(dns.Msg)

	err = reply.Unpack(body)
	if err != nil {
		return nil, err
	}

	// Hint network environment at successful connection.
	netenv.ReportSuccessfulConnection()

	// Report request duration for metrics.
	reportRequestDuration(queryStarted, hr.resolver)

	newRecord := &RRCache{
		Domain:   q.FQDN,
		Question: q.QType,
		RCode:    reply.Rcode,
		Answer:   reply.Answer,
		Ns:       reply.Ns,
		Extra:    reply.Extra,
		Resolver: hr.resolver.Info.Copy(),
	}

	// TODO: check if reply.Answer is valid
	return newRecord, nil
}

// ForceReconnect forces the resolver to re-establish the connection to the server.
func (hr *HTTPSResolver) ForceReconnect(ctx context.Context) {
	hr.refreshClient()
	log.Tracer(ctx).Tracef("resolver: created new HTTP client for %s", hr.resolver)
}

func (hr *HTTPSResolver) refreshClient() {
	// Lock client for changing.
	hr.clientLock.Lock()
	defer hr.clientLock.Unlock()

	// Attempt to close connection of previous client.
	if hr.client != nil {
		hr.client.CloseIdleConnections()
	}

	// Create new client.
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: hr.resolver.Info.Domain,
			// TODO: use portbase rng
		},
		IdleConnTimeout:     1 * time.Minute,
		TLSHandshakeTimeout: defaultConnectTimeout,
	}
	hr.client = &http.Client{
		Transport: tr,
		Timeout:   maxRequestTimeout,
	}
}
