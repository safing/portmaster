package resolver

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/tevino/abool"

	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/netutils"
)

// DNS Resolver Attributes
const (
	ServerTypeDNS  = "dns"
	ServerTypeTCP  = "tcp"
	ServerTypeDoT  = "dot"
	ServerTypeDoH  = "doh"
	ServerTypeMDNS = "mdns"
	ServerTypeEnv  = "env"

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
	ConfigURL string

	// Info holds the parsed configuration.
	Info *ResolverInfo

	// ServerAddress holds the resolver address for easier use.
	ServerAddress string

	// UpstreamBlockDetection defines the detection type
	// to identifier upstream DNS query blocking.
	// Valid values are:
	//	 - zeroip
	//	 - empty
	//   - refused (default)
	//	 - disabled
	UpstreamBlockDetection string

	// Special Options
	VerifyDomain string
	Search       []string

	// logic interface
	Conn ResolverConn `json:"-"`
}

// ResolverInfo is a subset of resolver attributes that is attached to answers
// from that server in order to use it later for decision making. It must not
// be changed by anyone after creation and initialization is complete.
type ResolverInfo struct {
	// Name describes the name given to the resolver. The name is configured in the config URL using the name parameter.
	Name string

	// Type describes the type of the resolver.
	// Possible values include dns, tcp, dot, doh, mdns, env.
	Type string

	// Source describes where the resolver configuration came from.
	// Possible values include config, system, mdns, env.
	Source string

	// IP is the IP address of the resolver
	IP net.IP

	// IPScope is the network scope of the IP address.
	IPScope netutils.IPScope

	// Port is the udp/tcp port of the resolver.
	Port uint16

	// id holds a unique ID for this resolver.
	id    string
	idGen sync.Once
}

// ID returns the unique ID of the resolver.
func (info *ResolverInfo) ID() string {
	// Generate the ID the first time.
	info.idGen.Do(func() {
		switch info.Type {
		case ServerTypeMDNS:
			info.id = ServerTypeMDNS
		case ServerTypeEnv:
			info.id = ServerTypeEnv
		default:
			info.id = fmt.Sprintf(
				"%s://%s:%d#%s",
				info.Type,
				info.IP,
				info.Port,
				info.Source,
			)
		}
	})

	return info.id
}

// DescriptiveName returns a human readable, but also detailed representation
// of the resolver.
func (info *ResolverInfo) DescriptiveName() string {
	switch {
	case info.Type == ServerTypeMDNS:
		return "MDNS"
	case info.Type == ServerTypeEnv:
		return "Portmaster Environment"
	case info.Name != "":
		return fmt.Sprintf(
			"%s (%s)",
			info.Name,
			info.ID(),
		)
	default:
		return fmt.Sprintf(
			"%s (%s)",
			info.IP.String(),
			info.ID(),
		)
	}
}

// Copy returns a full copy of the ResolverInfo.
func (info *ResolverInfo) Copy() *ResolverInfo {
	// Force idGen to run before we copy.
	_ = info.ID()

	// Copy manually in order to not copy the mutex.
	cp := &ResolverInfo{
		Name:    info.Name,
		Type:    info.Type,
		Source:  info.Source,
		IP:      info.IP,
		IPScope: info.IPScope,
		Port:    info.Port,
		id:      info.id,
	}
	// Trigger idGen.Do(), as the ID is already generated.
	cp.idGen.Do(func() {})

	return cp
}

// IsBlockedUpstream returns true if the request has been blocked
// upstream.
func (resolver *Resolver) IsBlockedUpstream(answer *dns.Msg) bool {
	return isBlockedUpstream(resolver, answer)
}

// String returns the URL representation of the resolver.
func (resolver *Resolver) String() string {
	return resolver.Info.DescriptiveName()
}

// ResolverConn is an interface to implement different types of query backends.
type ResolverConn interface { //nolint:go-lint // TODO
	Query(ctx context.Context, q *Query) (*RRCache, error)
	ReportFailure()
	IsFailing() bool
	ResetFailure()
}

// BasicResolverConn implements ResolverConn for standard dns clients.
type BasicResolverConn struct {
	sync.Mutex // for lastFail

	resolver *Resolver

	failing      *abool.AtomicBool
	failingUntil time.Time
	fails        int
	failLock     sync.Mutex

	networkChangedFlag *utils.Flag
}

// init initializes the basic resolver connection.
func (brc *BasicResolverConn) init() {
	brc.failing = abool.New()
	brc.networkChangedFlag = netenv.GetNetworkChangedFlag()
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
		brc.failing.Set()
		brc.failingUntil = time.Now().Add(time.Duration(nameserverRetryRate()) * time.Second)
		brc.fails = 0
	}
}

// IsFailing returns if this resolver is currently failing.
func (brc *BasicResolverConn) IsFailing() bool {
	// Check if not failing.
	if !brc.failing.IsSet() {
		return false
	}

	brc.Lock()
	defer brc.Unlock()

	// Reset failure status if the network changed since the last query.
	if brc.networkChangedFlag.IsSet() {
		brc.networkChangedFlag.Refresh()
		brc.ResetFailure()
		return false
	}

	// Check if we are still
	return time.Now().Before(brc.failingUntil)
}

// ResetFailure resets the failure status.
func (brc *BasicResolverConn) ResetFailure() {
	if brc.failing.SetToIf(true, false) {
		brc.Lock()
		defer brc.Unlock()
		brc.fails = 0
	}
}
