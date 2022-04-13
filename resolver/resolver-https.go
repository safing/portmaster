package resolver

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/miekg/dns"
)

// TCPResolver is a resolver using just a single tcp connection with pipelining.
type HttpsResolver struct {
	BasicResolverConn
}

// tcpQuery holds the query information for a tcpResolverConn.
type HttpsQuery struct {
	Query    *Query
	Response chan *dns.Msg
}

// MakeCacheRecord creates an RRCache record from a reply.
func (tq *HttpsQuery) MakeCacheRecord(reply *dns.Msg, resolverInfo *ResolverInfo) *RRCache {
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

// NewTCPResolver returns a new TPCResolver.
func NewHttpsResolver(resolver *Resolver) *HttpsResolver {
	newResolver := &HttpsResolver{
		BasicResolverConn: BasicResolverConn{
			resolver: resolver,
		},
	}
	newResolver.BasicResolverConn.init()
	return newResolver
}

// Query executes the given query against the resolver.
func (hr *HttpsResolver) Query(ctx context.Context, q *Query) (*RRCache, error) {
	// Get resolver connection.
	dnsQuery := new(dns.Msg)
	dnsQuery.SetQuestion(q.FQDN, uint16(q.QType))

	buf, err := dnsQuery.Pack()

	if err != nil {
		return nil, err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: hr.resolver.VerifyDomain,
			// TODO: use portbase rng
		},
	}

	b64dns := base64.RawStdEncoding.EncodeToString(buf)

	url := &url.URL{
		Scheme:     "https",
		Host:       hr.resolver.ServerAddress,
		Path:       fmt.Sprintf("%s/dns-query", hr.resolver.Path), // "dns-query" path is specified in rfc-8484 (https://www.rfc-editor.org/rfc/rfc8484.html)
		ForceQuery: true,
		RawQuery:   fmt.Sprintf("dns=%s", b64dns),
	}

	request := &http.Request{
		Method:     "GET",
		URL:        url,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       nil,
		Host:       hr.resolver.ServerAddress,
	}

	client := &http.Client{Transport: tr}

	resp, err := client.Do(request)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	reply := new(dns.Msg)
	reply.Unpack(body)

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
