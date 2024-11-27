package resolver

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
)

// DNS Resolver Attributes.
const (
	ServerTypeDNS      = "dns"
	ServerTypeTCP      = "tcp"
	ServerTypeDoT      = "dot"
	ServerTypeDoH      = "doh"
	ServerTypeMDNS     = "mdns"
	ServerTypeEnv      = "env"
	ServerTypeMonitor  = "monitor"
	ServerTypeFirewall = "firewall"

	ServerSourceConfigured      = "config"
	ServerSourceOperatingSystem = "system"
	ServerSourceMDNS            = "mdns"
	ServerSourceEnv             = "env"
	ServerSourceETW             = "etw"
	ServerSourceSystemd         = "systemd"
	ServerSourceFirewall        = "firewall"
)

// DNS resolver scheme aliases.
const (
	HTTPSProtocol = "https"
	TLSProtocol   = "tls"
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
	Search     []string
	SearchOnly bool
	Path       string
	// Special States
	LinkLocalUnavailable bool

	// logic interface
	Conn ResolverConn `json:"-"`
}

// ResolverInfo is a subset of resolver attributes that is attached to answers
// from that server in order to use it later for decision making. It must not
// be changed by anyone after creation and initialization is complete.
type ResolverInfo struct { //nolint:golint,maligned // TODO
	// Name describes the name given to the resolver. The name is configured in the config URL using the name parameter.
	Name string

	// Type describes the type of the resolver.
	// Possible values include dns, tcp, dot, doh, mdns, env, monitor, firewall.
	Type string

	// Source describes where the resolver configuration came from.
	// Possible values include config, system, mdns, env, etw, systemd, firewall.
	Source string

	// IP is the IP address of the resolver
	IP net.IP

	// Domain of the dns server if it has one
	Domain string

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
		case ServerTypeDoH:
			info.id = fmt.Sprintf( //nolint:nosprintfhostport // Not used as URL.
				"https://%s:%d#%s",
				info.Domain,
				info.Port,
				info.Source,
			)
		case ServerTypeDoT:
			info.id = fmt.Sprintf( //nolint:nosprintfhostport // Not used as URL.
				"dot://%s:%d#%s",
				info.Domain,
				info.Port,
				info.Source,
			)
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
	case info.Domain != "":
		return fmt.Sprintf(
			"%s (%s)",
			info.Domain,
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
		Domain:  info.Domain,
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
type ResolverConn interface { //nolint:golint // TODO
	Query(ctx context.Context, q *Query) (*RRCache, error)
	ReportFailure()
	IsFailing() bool
	ResetFailure()
	ForceReconnect(ctx context.Context)
}

// BasicResolverConn implements ResolverConn for standard dns clients.
type BasicResolverConn struct {
	sync.Mutex // Also used by inheriting structs.

	resolver *Resolver

	failing        *abool.AtomicBool
	failingStarted time.Time
	fails          int
	failLock       sync.Mutex

	networkChangedFlag *utils.Flag
}

// init initializes the basic resolver connection.
func (brc *BasicResolverConn) init() {
	brc.failing = abool.New()
	brc.networkChangedFlag = netenv.GetNetworkChangedFlag()
}
