package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/resolver"
	"github.com/safing/spn/navigator"
)

// FirewallHandler defines the function signature for a firewall
// handle function. A firewall handler is responsible for finding
// a reasonable verdict for the connection conn. The connection is
// locked before the firewall handler is called.
type FirewallHandler func(conn *Connection, pkt packet.Packet)

// ProcessContext holds additional information about the process
// that iniated a connection.
type ProcessContext struct {
	// ProcessName is the name of the process.
	ProcessName string
	// ProfileName is the name of the profile.
	ProfileName string
	// BinaryPath is the path to the process binary.
	BinaryPath string
	// CmdLine holds the execution parameters.
	CmdLine string
	// PID is the process identifier.
	PID int
	// Profile is the ID of the main profile that
	// is applied to the process.
	Profile string
	// Source is the source of the profile.
	Source string
}

// ConnectionType is a type of connection.
type ConnectionType int8

// Connection Types.
const (
	Undefined ConnectionType = iota
	IPConnection
	DNSRequest
)

// Connection describes a distinct physical network connection
// identified by the IP/Port pair.
type Connection struct { //nolint:maligned // TODO: fix alignment
	record.Base
	sync.Mutex

	// ID holds a unique request/connection id and is considered immutable after
	// creation.
	ID string
	// Type defines the connection type.
	Type ConnectionType
	// External defines if the connection represents an external request or
	// connection.
	External bool
	// Scope defines the scope of a connection. For DNS requests, the
	// scope is always set to the domain name. For direct packet
	// connections the scope consists of the involved network environment
	// and the packet direction. Once a connection object is created,
	// Scope is considered immutable.
	// Deprecated: This field holds duplicate information, which is accessible
	// clearer through other attributes. Please use conn.Type, conn.Inbound
	// and conn.Entity.Domain instead.
	Scope string
	// IPVersion is set to the packet IP version. It is not set (0) for
	// connections created from a DNS request.
	IPVersion packet.IPVersion
	// Inbound is set to true if the connection is incoming. Inbound is
	// only set when a connection object is created and is considered
	// immutable afterwards.
	Inbound bool
	// IPProtocol is set to the transport protocol used by the connection.
	// Is is considered immutable once a connection object has been
	// created. IPProtocol is not set for connections that have been
	// created from a DNS request.
	IPProtocol packet.IPProtocol
	// LocalIP holds the local IP address of the connection. It is not
	// set for connections created from DNS requests. LocalIP is
	// considered immutable once a connection object has been created.
	LocalIP net.IP
	// LocalIPScope holds the network scope of the local IP.
	LocalIPScope netutils.IPScope
	// LocalPort holds the local port of the connection. It is not
	// set for connections created from DNS requests. LocalPort is
	// considered immutable once a connection object has been created.
	LocalPort uint16
	// Entity describes the remote entity that the connection has been
	// established to. The entity might be changed or information might
	// be added to it during the livetime of a connection. Access to
	// entity must be guarded by the connection lock.
	Entity *intel.Entity
	// Resolver holds information about the resolver used to resolve
	// Entity.Domain.
	Resolver *resolver.ResolverInfo
	// Verdict holds the decisions that are made for a connection
	// The verdict may change so any access to it must be guarded by the
	// connection lock.
	Verdict struct {
		// Worst verdict holds the worst verdict that was assigned to this
		// connection from a privacy/security perspective.
		Worst Verdict
		// Active verdict holds the verdict that Portmaster will respond with.
		// This is different from the Firewall verdict in order to guarantee proper
		// transition between verdicts that need the connection to be re-established.
		Active Verdict
		// Firewall holsd the last (most recent) decision by the firewall.
		Firewall Verdict
	}
	// Reason holds information justifying the verdict, as well as additional
	// information about the reason.
	// Access to Reason must be guarded by the connection lock.
	Reason Reason
	// Started holds the number of seconds in UNIX epoch time at which
	// the connection has been initiated and first seen by the portmaster.
	// Started is only ever set when creating a new connection object
	// and is considered immutable afterwards.
	Started int64
	// Ended is set to the number of seconds in UNIX epoch time at which
	// the connection is considered terminated. Ended may be set at any
	// time so access must be guarded by the connection lock.
	Ended int64
	// VerdictPermanent is set to true if the final verdict is permanent
	// and the connection has been (or will be) handed back to the kernel.
	// VerdictPermanent may be changed together with the Verdict and Reason
	// properties and must be guarded using the connection lock.
	VerdictPermanent bool
	// Inspecting is set to true if the connection is being inspected
	// by one or more of the registered inspectors. This property may
	// be changed during the lifetime of a connection and must be guarded
	// using the connection lock.
	Inspecting bool
	// Tunneled is set to true when the connection has been routed through the
	// SPN.
	Tunneled bool
	// Encrypted is currently unused and MUST be ignored.
	Encrypted bool
	// TunnelOpts holds options for tunneling the connection.
	TunnelOpts *navigator.Options
	// ProcessContext holds additional information about the process
	// that initiated the connection. It is set once when the connection
	// object is created and is considered immutable afterwards.
	ProcessContext ProcessContext
	// DNSContext holds additional information about the DNS request that was
	// probably used to resolve the IP of this connection.
	DNSContext *resolver.DNSRequestContext
	// TunnelContext holds additional information about the tunnel that this
	// connection is using.
	TunnelContext interface {
		GetExitNodeID() string
		StopTunnel() error
	}

	// Internal is set to true if the connection is attributed as an
	// Portmaster internal connection. Internal may be set at different
	// points and access to it must be guarded by the connection lock.
	Internal bool
	// process holds a reference to the actor process. That is, the
	// process instance that initiated the connection.
	process *process.Process
	// pkgQueue is used to serialize packet handling for a single
	// connection and is served by the connections packetHandler.
	pktQueue chan packet.Packet
	// firewallHandler is the firewall handler that is called for
	// each packet sent to pktQueue.
	firewallHandler FirewallHandler
	// saveWhenFinished can be set to true during the life-time of
	// a connection and signals the firewallHandler that a Save()
	// should be issued after processing the connection.
	saveWhenFinished bool
	// activeInspectors is a slice of booleans where each entry
	// maps to the index of an available inspector. If the value
	// is true the inspector is currently active. False indicates
	// that the inspector has finished and should be skipped.
	activeInspectors []bool
	// inspectorData holds additional meta data for the inspectors.
	// using the inspectors index as a map key.
	inspectorData map[uint8]interface{}
	// ProfileRevisionCounter is used to track changes to the process
	// profile and required for correct re-evaluation of a connections
	// verdict.
	ProfileRevisionCounter uint64
	// addedToMetrics signifies if the connection has already been counted in
	// the metrics.
	addedToMetrics bool
}

// Reason holds information justifying a verdict, as well as additional
// information about the reason.
type Reason struct {
	// Msg is a human readable description of the reason.
	Msg string
	// OptionKey is the configuration option key of the setting that
	// was responsible for the verdict.
	OptionKey string
	// Profile is the database key of the profile that held the setting
	// that was responsible for the verdict.
	Profile string
	// ReasonContext may hold additional reason-specific information and
	// any access must be guarded by the connection lock.
	Context interface{}
}

func getProcessContext(ctx context.Context, proc *process.Process) ProcessContext {
	// Gather process information.
	pCtx := ProcessContext{
		ProcessName: proc.Name,
		BinaryPath:  proc.Path,
		CmdLine:     proc.CmdLine,
		PID:         proc.Pid,
	}

	// Get local profile.
	localProfile := proc.Profile().LocalProfile()
	if localProfile == nil {
		log.Tracer(ctx).Warningf("network: process %s has no profile", proc)
		return pCtx
	}

	// Add profile information and return.
	pCtx.ProfileName = localProfile.Name
	pCtx.Profile = localProfile.ID
	pCtx.Source = string(localProfile.Source)
	return pCtx
}

// NewConnectionFromDNSRequest returns a new connection based on the given dns request.
func NewConnectionFromDNSRequest(ctx context.Context, fqdn string, cnames []string, connID string, localIP net.IP, localPort uint16) *Connection {
	// Determine IP version.
	ipVersion := packet.IPv6
	if localIP.To4() != nil {
		ipVersion = packet.IPv4
	}

	// get Process
	proc, _, err := process.GetProcessByConnection(
		ctx,
		&packet.Info{
			Inbound:  false, // outbound as we are looking for the process of the source address
			Version:  ipVersion,
			Protocol: packet.UDP,
			Src:      localIP,   // source as in the process we are looking for
			SrcPort:  localPort, // source as in the process we are looking for
			Dst:      nil,       // do not record direction
			DstPort:  0,         // do not record direction
		},
	)
	if err != nil {
		log.Tracer(ctx).Debugf("network: failed to find process of dns request for %s: %s", fqdn, err)
		proc = process.GetUnidentifiedProcess(ctx)
	}

	timestamp := time.Now().Unix()
	dnsConn := &Connection{
		ID:    connID,
		Type:  DNSRequest,
		Scope: fqdn,
		Entity: &intel.Entity{
			Domain: fqdn,
			CNAME:  cnames,
		},
		process:        proc,
		ProcessContext: getProcessContext(ctx, proc),
		Started:        timestamp,
		Ended:          timestamp,
	}

	// Inherit internal status of profile.
	if localProfile := proc.Profile().LocalProfile(); localProfile != nil {
		dnsConn.Internal = localProfile.Internal
	}

	// DNS Requests are saved by the nameserver depending on the result of the
	// query. Blocked requests are saved immediately, accepted ones are only
	// saved if they are not "used" by a connection.

	return dnsConn
}

// NewConnectionFromExternalDNSRequest returns a connection for an external DNS request.
func NewConnectionFromExternalDNSRequest(ctx context.Context, fqdn string, cnames []string, connID string, remoteIP net.IP) (*Connection, error) {
	remoteHost, err := process.GetNetworkHost(ctx, remoteIP)
	if err != nil {
		return nil, err
	}

	timestamp := time.Now().Unix()
	dnsConn := &Connection{
		ID:       connID,
		Type:     DNSRequest,
		External: true,
		Scope:    fqdn,
		Entity: &intel.Entity{
			Domain: fqdn,
			CNAME:  cnames,
		},
		process:        remoteHost,
		ProcessContext: getProcessContext(ctx, remoteHost),
		Started:        timestamp,
		Ended:          timestamp,
	}

	// Inherit internal status of profile.
	if localProfile := remoteHost.Profile().LocalProfile(); localProfile != nil {
		dnsConn.Internal = localProfile.Internal
	}

	// DNS Requests are saved by the nameserver depending on the result of the
	// query. Blocked requests are saved immediately, accepted ones are only
	// saved if they are not "used" by a connection.

	return dnsConn, nil
}

// NewConnectionFromFirstPacket returns a new connection based on the given packet.
func NewConnectionFromFirstPacket(pkt packet.Packet) *Connection {
	// get Process
	proc, inbound, err := process.GetProcessByConnection(pkt.Ctx(), pkt.Info())
	if err != nil {
		log.Tracer(pkt.Ctx()).Debugf("network: failed to find process of packet %s: %s", pkt, err)
		if inbound {
			proc = process.GetUnsolicitedProcess(pkt.Ctx())
		} else {
			proc = process.GetUnidentifiedProcess(pkt.Ctx())
		}
	}

	// Create the (remote) entity.
	entity := &intel.Entity{
		Protocol: uint8(pkt.Info().Protocol),
		Port:     pkt.Info().RemotePort(),
	}
	entity.SetIP(pkt.Info().RemoteIP())
	entity.SetDstPort(pkt.Info().DstPort)

	var scope string
	var resolverInfo *resolver.ResolverInfo
	var dnsContext *resolver.DNSRequestContext

	if inbound {
		switch entity.IPScope {
		case netutils.HostLocal:
			scope = IncomingHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			scope = IncomingLAN
		case netutils.Global, netutils.GlobalMulticast:
			scope = IncomingInternet

		case netutils.Undefined, netutils.Invalid:
			fallthrough
		default:
			scope = IncomingInvalid
		}
	} else {

		// check if we can find a domain for that IP
		ipinfo, err := resolver.GetIPInfo(proc.Profile().LocalProfile().ID, pkt.Info().RemoteIP().String())
		if err != nil {
			// Try again with the global scope, in case DNS went through the system resolver.
			ipinfo, err = resolver.GetIPInfo(resolver.IPInfoProfileScopeGlobal, pkt.Info().RemoteIP().String())
		}
		if err == nil {
			lastResolvedDomain := ipinfo.MostRecentDomain()
			if lastResolvedDomain != nil {
				scope = lastResolvedDomain.Domain
				entity.Domain = lastResolvedDomain.Domain
				entity.CNAME = lastResolvedDomain.CNAMEs
				dnsContext = lastResolvedDomain.DNSRequestContext
				resolverInfo = lastResolvedDomain.Resolver
				removeOpenDNSRequest(proc.Pid, lastResolvedDomain.Domain)
			}
		}

		// check if destination IP is the captive portal's IP
		portal := netenv.GetCaptivePortal()
		if pkt.Info().RemoteIP().Equal(portal.IP) {
			scope = portal.Domain
			entity.Domain = portal.Domain
		}

		if scope == "" {
			// outbound direct (possibly P2P) connection
			switch entity.IPScope {
			case netutils.HostLocal:
				scope = PeerHost
			case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
				scope = PeerLAN
			case netutils.Global, netutils.GlobalMulticast:
				scope = PeerInternet

			case netutils.Undefined, netutils.Invalid:
				fallthrough
			default:
				scope = PeerInvalid
			}
		}
	}

	// Create new connection object.
	newConn := &Connection{
		ID:        pkt.GetConnectionID(),
		Type:      IPConnection,
		Scope:     scope,
		IPVersion: pkt.Info().Version,
		Inbound:   inbound,
		// local endpoint
		IPProtocol:     pkt.Info().Protocol,
		LocalPort:      pkt.Info().LocalPort(),
		ProcessContext: getProcessContext(pkt.Ctx(), proc),
		DNSContext:     dnsContext,
		process:        proc,
		// remote endpoint
		Entity: entity,
		// resolver used to resolve dns request
		Resolver: resolverInfo,
		// meta
		Started:                time.Now().Unix(),
		ProfileRevisionCounter: proc.Profile().RevisionCnt(),
	}
	newConn.SetLocalIP(pkt.Info().LocalIP())

	// Inherit internal status of profile.
	if localProfile := proc.Profile().LocalProfile(); localProfile != nil {
		newConn.Internal = localProfile.Internal
	}

	// Save connection to internal state in order to mitigate creation of
	// duplicates. Do not propagate yet, as there is no verdict yet.
	conns.add(newConn)

	return newConn
}

// GetConnection fetches a Connection from the database.
func GetConnection(id string) (*Connection, bool) {
	return conns.get(id)
}

// GetAllConnections Gets all connection.
func GetAllConnections() []*Connection {
	return conns.list()
}

// SetLocalIP sets the local IP address together with its network scope. The
// connection is not locked for this.
func (conn *Connection) SetLocalIP(ip net.IP) {
	conn.LocalIP = ip
	conn.LocalIPScope = netutils.GetIPScope(ip)
}

// AcceptWithContext accepts the connection.
func (conn *Connection) AcceptWithContext(reason, reasonOptionKey string, ctx interface{}) {
	if !conn.SetVerdict(VerdictAccept, reason, reasonOptionKey, ctx) {
		log.Warningf("filter: tried to accept %s, but current verdict is %s", conn, conn.Verdict)
	}
}

// Accept is like AcceptWithContext but only accepts a reason.
func (conn *Connection) Accept(reason, reasonOptionKey string) {
	conn.AcceptWithContext(reason, reasonOptionKey, nil)
}

// BlockWithContext blocks the connection.
func (conn *Connection) BlockWithContext(reason, reasonOptionKey string, ctx interface{}) {
	if !conn.SetVerdict(VerdictBlock, reason, reasonOptionKey, ctx) {
		log.Warningf("filter: tried to block %s, but current verdict is %s", conn, conn.Verdict)
	}
}

// Block is like BlockWithContext but does only accepts a reason.
func (conn *Connection) Block(reason, reasonOptionKey string) {
	conn.BlockWithContext(reason, reasonOptionKey, nil)
}

// DropWithContext drops the connection.
func (conn *Connection) DropWithContext(reason, reasonOptionKey string, ctx interface{}) {
	if !conn.SetVerdict(VerdictDrop, reason, reasonOptionKey, ctx) {
		log.Warningf("filter: tried to drop %s, but current verdict is %s", conn, conn.Verdict)
	}
}

// Drop is like DropWithContext but does only accepts a reason.
func (conn *Connection) Drop(reason, reasonOptionKey string) {
	conn.DropWithContext(reason, reasonOptionKey, nil)
}

// DenyWithContext blocks or drops the link depending on the connection direction.
func (conn *Connection) DenyWithContext(reason, reasonOptionKey string, ctx interface{}) {
	if conn.Inbound {
		conn.DropWithContext(reason, reasonOptionKey, ctx)
	} else {
		conn.BlockWithContext(reason, reasonOptionKey, ctx)
	}
}

// Deny is like DenyWithContext but only accepts a reason.
func (conn *Connection) Deny(reason, reasonOptionKey string) {
	conn.DenyWithContext(reason, reasonOptionKey, nil)
}

// FailedWithContext marks the connection with VerdictFailed and stores the reason.
func (conn *Connection) FailedWithContext(reason, reasonOptionKey string, ctx interface{}) {
	if !conn.SetVerdict(VerdictFailed, reason, reasonOptionKey, ctx) {
		log.Warningf("filter: tried to drop %s due to error but current verdict is %s", conn, conn.Verdict)
	}
}

// Failed is like FailedWithContext but only accepts a string.
func (conn *Connection) Failed(reason, reasonOptionKey string) {
	conn.FailedWithContext(reason, reasonOptionKey, nil)
}

// SetVerdict sets a new verdict for the connection.
func (conn *Connection) SetVerdict(newVerdict Verdict, reason, reasonOptionKey string, reasonCtx interface{}) (ok bool) {
	conn.SetVerdictDirectly(newVerdict)

	conn.Reason.Msg = reason
	conn.Reason.Context = reasonCtx

	conn.Reason.OptionKey = ""
	conn.Reason.Profile = ""
	if reasonOptionKey != "" && conn.Process() != nil {
		conn.Reason.OptionKey = reasonOptionKey
		conn.Reason.Profile = conn.Process().Profile().GetProfileSource(conn.Reason.OptionKey)
	}

	return true // TODO: remove
}

// SetVerdictDirectly sets the firewall verdict.
func (conn *Connection) SetVerdictDirectly(newVerdict Verdict) {
	conn.Verdict.Firewall = newVerdict
}

// VerdictVerb returns the verdict as a verb, while taking any special states
// into account.
func (conn *Connection) VerdictVerb() string {
	if conn.Verdict.Firewall == conn.Verdict.Active {
		return conn.Verdict.Firewall.Verb()
	}
	return fmt.Sprintf(
		"%s (transitioning to %s)",
		conn.Verdict.Active.Verb(),
		conn.Verdict.Firewall.Verb(),
	)
}

// Process returns the connection's process.
func (conn *Connection) Process() *process.Process {
	return conn.process
}

// SaveWhenFinished marks the connection for saving it after the firewall handler.
func (conn *Connection) SaveWhenFinished() {
	conn.saveWhenFinished = true
}

// Save saves the connection in the storage and propagates the change
// through the database system. Save may lock dnsConnsLock or connsLock
// in if Save() is called the first time.
// Callers must make sure to lock the connection itself before calling
// Save().
func (conn *Connection) Save() {
	conn.addToMetrics()
	conn.UpdateMeta()

	if !conn.KeyIsSet() {
		if conn.Type == DNSRequest {
			conn.SetKey(makeKey(conn.process.Pid, dbScopeDNS, conn.ID))
			dnsConns.add(conn)
		} else {
			conn.SetKey(makeKey(conn.process.Pid, dbScopeIP, conn.ID))
			conns.add(conn)
		}
	}

	// notify database controller
	dbController.PushUpdate(conn)
}

// delete deletes a link from the storage and propagates the change.
// delete may lock either the dnsConnsLock or connsLock. Callers
// must still make sure to lock the connection itself.
func (conn *Connection) delete() {
	// A connection without an ID has been created from
	// a DNS request rather than a packet. Choose the correct
	// connection store here.
	if conn.Type == IPConnection {
		conns.delete(conn)
	} else {
		dnsConns.delete(conn)
	}

	conn.Meta().Delete()
	dbController.PushUpdate(conn)
}

// SetFirewallHandler sets the firewall handler for this link, and starts a
// worker to handle the packets.
// The caller needs to hold a lock on the connection.
func (conn *Connection) SetFirewallHandler(handler FirewallHandler) {
	if conn.firewallHandler == nil {
		conn.pktQueue = make(chan packet.Packet, 100)

		// start handling
		module.StartWorker("packet handler", conn.packetHandlerWorker)
	}
	conn.firewallHandler = handler
}

// StopFirewallHandler unsets the firewall handler and stops the handler worker.
// The caller needs to hold a lock on the connection.
func (conn *Connection) StopFirewallHandler() {
	conn.firewallHandler = nil

	// Signal the packet handler worker that it can stop.
	close(conn.pktQueue)

	// Unset the packet queue so that it can be freed.
	conn.pktQueue = nil
}

// HandlePacket queues packet of Link for handling.
func (conn *Connection) HandlePacket(pkt packet.Packet) {
	conn.Lock()
	defer conn.Unlock()

	// execute handler or verdict
	if conn.firewallHandler != nil {
		conn.pktQueue <- pkt
		// TODO: drop if overflowing?
	} else {
		defaultFirewallHandler(conn, pkt)
	}
}

// packetHandlerWorker sequentially handles queued packets.
func (conn *Connection) packetHandlerWorker(ctx context.Context) error {
	// Copy packet queue, so we can remove the reference from the connection
	// when we stop the firewall handler.
	pktQueue := conn.pktQueue

	for {
		select {
		case pkt := <-pktQueue:
			if pkt == nil {
				return nil
			}
			packetHandlerHandleConn(conn, pkt)

		case <-ctx.Done():
			conn.Lock()
			defer conn.Unlock()
			conn.firewallHandler = nil
			return nil
		}
	}
}

func packetHandlerHandleConn(conn *Connection, pkt packet.Packet) {
	conn.Lock()
	defer conn.Unlock()

	// Handle packet with appropriate handler.
	if conn.firewallHandler != nil {
		conn.firewallHandler(conn, pkt)
	} else {
		defaultFirewallHandler(conn, pkt)
	}

	// Log verdict.
	log.Tracer(pkt.Ctx()).Infof("filter: connection %s %s: %s", conn, conn.VerdictVerb(), conn.Reason.Msg)
	// Submit trace logs.
	log.Tracer(pkt.Ctx()).Submit()

	// Save() itself does not touch any changing data.
	// Must not be locked - would deadlock with cleaner functions.
	if conn.saveWhenFinished {
		conn.saveWhenFinished = false
		conn.Save()
	}
}

// GetActiveInspectors returns the list of active inspectors.
func (conn *Connection) GetActiveInspectors() []bool {
	return conn.activeInspectors
}

// SetActiveInspectors sets the list of active inspectors.
func (conn *Connection) SetActiveInspectors(newInspectors []bool) {
	conn.activeInspectors = newInspectors
}

// GetInspectorData returns the list of inspector data.
func (conn *Connection) GetInspectorData() map[uint8]interface{} {
	return conn.inspectorData
}

// SetInspectorData set the list of inspector data.
func (conn *Connection) SetInspectorData(newInspectorData map[uint8]interface{}) {
	conn.inspectorData = newInspectorData
}

// String returns a string representation of conn.
func (conn *Connection) String() string {
	switch {
	case conn.Inbound:
		return fmt.Sprintf("%s <- %s", conn.process, conn.Entity.IP)
	case conn.Entity.Domain != "":
		return fmt.Sprintf("%s to %s (%s)", conn.process, conn.Entity.Domain, conn.Entity.IP)
	default:
		return fmt.Sprintf("%s -> %s", conn.process, conn.Entity.IP)
	}
}
