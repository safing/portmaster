package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/safing/portmaster/netenv"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/resolver"
)

// FirewallHandler defines the function signature for a firewall
// handle function. A firewall handler is responsible for finding
// a reasonable verdict for the connection conn. The connection is
// locked before the firewall handler is called.
type FirewallHandler func(conn *Connection, pkt packet.Packet)

// ProcessContext holds additional information about the process
// that iniated a connection.
type ProcessContext struct {
	// Name is the name of the process.
	Name string
	// BinaryPath is the path to the process binary.
	BinaryPath string
	// PID i the process identifier.
	PID int
	// ProfileID is the ID of the main profile that
	// is applied to the process.
	ProfileID string
}

// Connection describes a distinct physical network connection
// identified by the IP/Port pair.
type Connection struct { //nolint:maligned // TODO: fix alignment
	record.Base
	sync.Mutex

	// ID may hold unique connection id. It is only set for non-DNS
	// request connections and is considered immutable after a
	// connection object has been created.
	ID string
	// Scope defines the scope of a connection. For DNS requests, the
	// scope is always set to the domain name. For direct packet
	// connections the scope consists of the involved network environment
	// and the packet direction. Once a connection object is created,
	// Scope is considered immutable.
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
	// LocalPort holds the local port of the connection. It is not
	// set for connections created from DNS requests. LocalPort is
	// considered immutable once a connection object has been created.
	LocalPort uint16
	// Entity describes the remote entity that the connection has been
	// established to. The entity might be changed or information might
	// be added to it during the livetime of a connection. Access to
	// entity must be guarded by the connection lock.
	Entity *intel.Entity
	// Verdict is the final decision that has been made for a connection.
	// The verdict may change so any access to it must be guarded by the
	// connection lock.
	Verdict Verdict
	// Reason holds information justifying the verdict, as well as additional
	// information about the reason.
	// Access to Reason must be guarded by the connection lock.
	Reason Reason
	// Started holds the number of seconds in UNIX epoch time at which
	// the connection has been initated and first seen by the portmaster.
	// Staretd is only every set when creating a new connection object
	// and is considered immutable afterwards.
	Started int64
	// Ended is set to the number of seconds in UNIX epoch time at which
	// the connection is considered terminated. Ended may be set at any
	// time so access must be guarded by the conneciton lock.
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
	// Tunneled is currently unused and MUST be ignored.
	Tunneled bool
	// Encrypted is currently unused and MUST be ignored.
	Encrypted bool
	// ProcessContext holds additional information about the process
	// that iniated the connection. It is set once when the connection
	// object is created and is considered immutable afterwards.
	ProcessContext ProcessContext
	// Internal is set to true if the connection is attributed as an
	// Portmaster internal connection. Internal may be set at different
	// points and access to it must be guarded by the connection lock.
	Internal bool
	// process holds a reference to the actor process. That is, the
	// process instance that initated the conneciton.
	process *process.Process
	// pkgQueue is used to serialize packet handling for a single
	// connection and is served by the connections packetHandler.
	pktQueue chan packet.Packet
	// firewallHandler is the firewall handler that is called for
	// each packet sent to pktQueue.
	firewallHandler FirewallHandler
	// saveWhenFinished can be set to drue during the life-time of
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

func getProcessContext(proc *process.Process) ProcessContext {
	return ProcessContext{
		BinaryPath: proc.Path,
		Name:       proc.Name,
		PID:        proc.Pid,
		ProfileID:  proc.LocalProfileKey,
	}
}

// NewConnectionFromDNSRequest returns a new connection based on the given dns request.
func NewConnectionFromDNSRequest(ctx context.Context, fqdn string, cnames []string, ipVersion packet.IPVersion, localIP net.IP, localPort uint16) *Connection {
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
		Scope: fqdn,
		Entity: &intel.Entity{
			Domain: fqdn,
			CNAME:  cnames,
		},
		process:        proc,
		ProcessContext: getProcessContext(proc),
		Started:        timestamp,
		Ended:          timestamp,
	}
	return dnsConn
}

// NewConnectionFromFirstPacket returns a new connection based on the given packet.
func NewConnectionFromFirstPacket(pkt packet.Packet) *Connection {
	// get Process
	proc, inbound, err := process.GetProcessByConnection(pkt.Ctx(), pkt.Info())
	if err != nil {
		log.Tracer(pkt.Ctx()).Debugf("network: failed to find process of packet %s: %s", pkt, err)
		proc = process.GetUnidentifiedProcess(pkt.Ctx())
	}

	var scope string
	var entity *intel.Entity

	if inbound {

		// inbound connection
		switch netutils.ClassifyIP(pkt.Info().Src) {
		case netutils.HostLocal:
			scope = IncomingHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			scope = IncomingLAN
		case netutils.Global, netutils.GlobalMulticast:
			scope = IncomingInternet

		case netutils.Invalid:
			fallthrough
		default:
			scope = IncomingInvalid
		}
		entity = &intel.Entity{
			IP:       pkt.Info().Src,
			Protocol: uint8(pkt.Info().Protocol),
			Port:     pkt.Info().SrcPort,
		}
		entity.SetDstPort(pkt.Info().DstPort)

	} else {

		// outbound connection
		entity = &intel.Entity{
			IP:       pkt.Info().Dst,
			Protocol: uint8(pkt.Info().Protocol),
			Port:     pkt.Info().DstPort,
		}
		entity.SetDstPort(entity.Port)

		// check if we can find a domain for that IP
		ipinfo, err := resolver.GetIPInfo(proc.LocalProfileKey, pkt.Info().Dst.String())
		if err == nil {
			lastResolvedDomain := ipinfo.MostRecentDomain()
			if lastResolvedDomain != nil {
				scope = lastResolvedDomain.Domain
				entity.Domain = lastResolvedDomain.Domain
				entity.CNAME = lastResolvedDomain.CNAMEs
				removeOpenDNSRequest(proc.Pid, lastResolvedDomain.Domain)
			}
		}

		// check if destination IP is the captive portal's IP
		portal := netenv.GetCaptivePortal()
		if pkt.Info().Dst.Equal(portal.IP) {
			scope = portal.Domain
			entity.Domain = portal.Domain
		}

		if scope == "" {

			// outbound direct (possibly P2P) connection
			switch netutils.ClassifyIP(pkt.Info().Dst) {
			case netutils.HostLocal:
				scope = PeerHost
			case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
				scope = PeerLAN
			case netutils.Global, netutils.GlobalMulticast:
				scope = PeerInternet

			case netutils.Invalid:
				fallthrough
			default:
				scope = PeerInvalid
			}

		}
	}

	return &Connection{
		ID:        pkt.GetConnectionID(),
		Scope:     scope,
		IPVersion: pkt.Info().Version,
		Inbound:   inbound,
		// local endpoint
		IPProtocol:     pkt.Info().Protocol,
		LocalIP:        pkt.Info().LocalIP(),
		LocalPort:      pkt.Info().LocalPort(),
		ProcessContext: getProcessContext(proc),
		process:        proc,
		// remote endpoint
		Entity: entity,
		// meta
		Started:                time.Now().Unix(),
		ProfileRevisionCounter: proc.Profile().RevisionCnt(),
	}
}

// GetConnection fetches a Connection from the database.
func GetConnection(id string) (*Connection, bool) {
	return conns.get(id)
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

// SetVerdict sets a new verdict for the connection, making sure it does not interfere with previous verdicts.
func (conn *Connection) SetVerdict(newVerdict Verdict, reason, reasonOptionKey string, reasonCtx interface{}) (ok bool) {
	if newVerdict >= conn.Verdict {
		conn.Verdict = newVerdict
		conn.Reason.Msg = reason
		conn.Reason.Context = reasonCtx
		if reasonOptionKey != "" && conn.Process() != nil {
			conn.Reason.OptionKey = reasonOptionKey
			conn.Reason.Profile = conn.Process().Profile().GetProfileSource(conn.Reason.OptionKey)
		}
		return true
	}
	return false
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
	conn.UpdateMeta()

	if !conn.KeyIsSet() {
		// A connection without an ID has been created from
		// a DNS request rather than a packet. Choose the correct
		// connection store here.
		if conn.ID == "" {
			conn.SetKey(fmt.Sprintf("network:tree/%d/%s", conn.process.Pid, conn.Scope))
			dnsConns.add(conn)
		} else {
			conn.SetKey(fmt.Sprintf("network:tree/%d/%s/%s", conn.process.Pid, conn.Scope, conn.ID))
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
	if conn.ID == "" {
		dnsConns.delete(conn)
	} else {
		conns.delete(conn)
	}

	conn.Meta().Delete()
	dbController.PushUpdate(conn)
}

// UpdateAndCheck updates profiles and checks whether a reevaluation is needed.
func (conn *Connection) UpdateAndCheck() (needsReevaluation bool) {
	p := conn.process.Profile()
	if p == nil {
		return false
	}
	revCnt := p.Update()

	if conn.profileRevisionCounter != revCnt {
		conn.profileRevisionCounter = revCnt
		needsReevaluation = true
	}
	return
}

// SetFirewallHandler sets the firewall handler for this link, and starts a
// worker to handle the packets.
func (conn *Connection) SetFirewallHandler(handler FirewallHandler) {
	if conn.firewallHandler == nil {
		conn.pktQueue = make(chan packet.Packet, 1000)

		// start handling
		module.StartWorker("packet handler", func(ctx context.Context) error {
			conn.packetHandler()
			return nil
		})
	}
	conn.firewallHandler = handler
}

// StopFirewallHandler unsets the firewall handler and stops the handler worker.
func (conn *Connection) StopFirewallHandler() {
	conn.firewallHandler = nil
	conn.pktQueue <- nil
}

// HandlePacket queues packet of Link for handling
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

// packetHandler sequentially handles queued packets
func (conn *Connection) packetHandler() {
	for pkt := range conn.pktQueue {
		if pkt == nil {
			return
		}
		// get handler
		conn.Lock()

		// execute handler or verdict
		if conn.firewallHandler != nil {
			conn.firewallHandler(conn, pkt)
		} else {
			defaultFirewallHandler(conn, pkt)
		}
		// log verdict
		log.Tracer(pkt.Ctx()).Infof("filter: connection %s %s: %s", conn, conn.Verdict.Verb(), conn.Reason.Msg)

		// save does not touch any changing data
		// must not be locked, will deadlock with cleaner functions
		if conn.saveWhenFinished {
			conn.saveWhenFinished = false
			conn.Save()
		}

		conn.Unlock()
		// submit trace logs
		log.Tracer(pkt.Ctx()).Submit()
	}
}

// GetActiveInspectors returns the list of active inspectors.
func (conn *Connection) GetActiveInspectors() []bool {
	return conn.activeInspectors
}

// SetActiveInspectors sets the list of active inspectors.
func (conn *Connection) SetActiveInspectors(new []bool) {
	conn.activeInspectors = new
}

// GetInspectorData returns the list of inspector data.
func (conn *Connection) GetInspectorData() map[uint8]interface{} {
	return conn.inspectorData
}

// SetInspectorData set the list of inspector data.
func (conn *Connection) SetInspectorData(new map[uint8]interface{}) {
	conn.inspectorData = new
}

// String returns a string representation of conn.
func (conn *Connection) String() string {
	switch conn.Scope {
	case IncomingHost, IncomingLAN, IncomingInternet, IncomingInvalid:
		return fmt.Sprintf("%s <- %s", conn.process, conn.Entity.IP)
	case PeerHost, PeerLAN, PeerInternet, PeerInvalid:
		return fmt.Sprintf("%s -> %s", conn.process, conn.Entity.IP)
	default:
		return fmt.Sprintf("%s to %s (%s)", conn.process, conn.Entity.Domain, conn.Entity.IP)
	}
}
