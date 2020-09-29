package resolver

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/safing/portmaster/netenv"
)

// DNS Resolver Attributes
const (
	ServerTypeDNS = "dns"
	ServerTypeTCP = "tcp"
	ServerTypeDoT = "dot"
	ServerTypeDoH = "doh"
	ServerTypeEnv = "env"

	ServerSourceConfigured      = "config"
	ServerSourceOperatingSystem = "system"
	ServerSourceMDNS            = "mdns"
	ServerSourceEnv             = "env"
)

var (
	// FailThreshold is amount of errors a resolvers must experience in order to be regarded as failed.
	FailThreshold = 20
)

// Resolver holds information about an active resolver.
type Resolver struct {
	// Server config url (and ID)
	// Supported parameters:
	// - `verify=domain`: verify domain (dot only)
	// - `name=name`: human readable name for resolver
	// - `blockedif=empty`: how to detect if the dns service blocked something
	//	- `empty`: NXDomain result, but without any other record in any section
	//  - `refused`: Request was refused
	//	- `zeroip`: Answer only contains zeroip
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
	ServerInfo    string

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

	resolver *Resolver

	failingUntil time.Time
	fails        int
}

// ReportFailure reports that an error occurred with this resolver.
func (brc *BasicResolverConn) ReportFailure() {
	if !netenv.Online() {
		// don't mark failed if we are offline
		return
	}

	brc.Lock()
	defer brc.Unlock()

	brc.fails++
	if brc.fails > FailThreshold {
		brc.failingUntil = time.Now().Add(time.Duration(nameserverRetryRate()) * time.Second)
		brc.fails = 0
	}
}

// IsFailing returns if this resolver is currently failing.
func (brc *BasicResolverConn) IsFailing() bool {
	brc.Lock()
	defer brc.Unlock()

	return time.Now().Before(brc.failingUntil)
}
