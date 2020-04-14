package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/resolver"
)

// FirewallHandler defines the function signature for a firewall handle function
type FirewallHandler func(conn *Connection, pkt packet.Packet)

// Connection describes a distinct physical network connection identified by the IP/Port pair.
type Connection struct { //nolint:maligned // TODO: fix alignment
	record.Base
	sync.Mutex

	ID      string
	Scope   string
	Inbound bool
	Entity  *intel.Entity // needs locking, instance is never shared
	process *process.Process

	Verdict  Verdict
	Reason   string
	ReasonID string // format source[:id[:id]] // TODO

	Started          int64
	Ended            int64
	Tunneled         bool
	VerdictPermanent bool
	Inspecting       bool
	Encrypted        bool // TODO

	pktQueue        chan packet.Packet
	firewallHandler FirewallHandler

	activeInspectors []bool
	inspectorData    map[uint8]interface{}

	saveWhenFinished       bool
	profileRevisionCounter uint64
}

// NewConnectionFromDNSRequest returns a new connection based on the given dns request.
func NewConnectionFromDNSRequest(ctx context.Context, fqdn string, ip net.IP, port uint16) *Connection {
	// get Process
	proc, err := process.GetProcessByEndpoints(ctx, ip, port, dnsAddress, dnsPort, packet.UDP)
	if err != nil {
		log.Warningf("network: failed to find process of dns request for %s: %s", fqdn, err)
		proc = process.UnknownProcess
	}

	timestamp := time.Now().Unix()
	dnsConn := &Connection{
		Scope: fqdn,
		Entity: (&intel.Entity{
			Domain: fqdn,
		}).Init(),
		process: proc,
		Started: timestamp,
		Ended:   timestamp,
	}
	return dnsConn
}

// NewConnectionFromFirstPacket returns a new connection based on the given packet.
func NewConnectionFromFirstPacket(pkt packet.Packet) *Connection {
	// get Process
	proc, inbound, err := process.GetProcessByPacket(pkt)
	if err != nil {
		log.Warningf("network: failed to find process of packet %s: %s", pkt, err)
		proc = process.UnknownProcess
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
		default: // netutils.Invalid
			scope = IncomingInvalid
		}
		entity = (&intel.Entity{
			IP:       pkt.Info().Src,
			Protocol: uint8(pkt.Info().Protocol),
			Port:     pkt.Info().SrcPort,
		}).Init()

	} else {

		// outbound connection
		entity = (&intel.Entity{
			IP:       pkt.Info().Dst,
			Protocol: uint8(pkt.Info().Protocol),
			Port:     pkt.Info().DstPort,
		}).Init()

		// check if we can find a domain for that IP
		ipinfo, err := resolver.GetIPInfo(pkt.Info().Dst.String())
		if err == nil {

			// outbound to domain
			scope = ipinfo.Domains[0]
			entity.Domain = scope
			removeOpenDNSRequest(proc.Pid, scope)

		} else {

			// outbound direct (possibly P2P) connection
			switch netutils.ClassifyIP(pkt.Info().Dst) {
			case netutils.HostLocal:
				scope = PeerHost
			case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
				scope = PeerLAN
			case netutils.Global, netutils.GlobalMulticast:
				scope = PeerInternet
			default: // netutils.Invalid
				scope = PeerInvalid
			}

		}
	}

	timestamp := time.Now().Unix()
	return &Connection{
		ID:      pkt.GetConnectionID(),
		Scope:   scope,
		Entity:  entity,
		process: proc,
		Started: timestamp,
	}
}

// GetConnection fetches a Connection from the database.
func GetConnection(id string) (*Connection, bool) {
	connsLock.RLock()
	defer connsLock.RUnlock()

	conn, ok := conns[id]
	return conn, ok
}

// Accept accepts the connection.
func (conn *Connection) Accept(reason string) {
	if conn.SetVerdict(VerdictAccept) {
		conn.Reason = reason
		log.Infof("filter: granting connection %s, %s", conn, conn.Reason)
	} else {
		log.Warningf("filter: tried to accept %s, but current verdict is %s", conn, conn.Verdict)
	}
}

// Block blocks the connection.
func (conn *Connection) Block(reason string) {
	if conn.SetVerdict(VerdictBlock) {
		conn.Reason = reason
		log.Infof("filter: blocking connection %s, %s", conn, conn.Reason)
	} else {
		log.Warningf("filter: tried to block %s, but current verdict is %s", conn, conn.Verdict)
	}
}

// Drop drops the connection.
func (conn *Connection) Drop(reason string) {
	if conn.SetVerdict(VerdictDrop) {
		conn.Reason = reason
		log.Infof("filter: dropping connection %s, %s", conn, conn.Reason)
	} else {
		log.Warningf("filter: tried to drop %s, but current verdict is %s", conn, conn.Verdict)
	}
}

// Deny blocks or drops the link depending on the connection direction.
func (conn *Connection) Deny(reason string) {
	if conn.Inbound {
		conn.Drop(reason)
	} else {
		conn.Block(reason)
	}
}

// Failed marks the connection with VerdictFailed and stores the reason.
func (conn *Connection) Failed(reason string) {
	if conn.SetVerdict(VerdictFailed) {
		conn.Reason = reason
		log.Infof("filter: dropping connection %s because of an internal error: %s", conn, reason)
	} else {
		log.Warningf("filter: tried to drop %s due to error but current verdict is %s", conn, conn.Verdict)
	}
}

// SetVerdict sets a new verdict for the connection, making sure it does not interfere with previous verdicts.
func (conn *Connection) SetVerdict(newVerdict Verdict) (ok bool) {
	if newVerdict >= conn.Verdict {
		conn.Verdict = newVerdict
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

// Save saves the connection in the storage and propagates the change through the database system.
func (conn *Connection) Save() {
	if conn.ID == "" {

		// dns request
		if !conn.KeyIsSet() {
			conn.SetKey(fmt.Sprintf("network:tree/%d/%s", conn.process.Pid, conn.Scope))
			conn.UpdateMeta()
		}
		// save to internal state
		// check if it already exists
		mapKey := strconv.Itoa(conn.process.Pid) + "/" + conn.Scope
		dnsConnsLock.Lock()
		_, ok := dnsConns[mapKey]
		if !ok {
			dnsConns[mapKey] = conn
		}
		dnsConnsLock.Unlock()

	} else {

		// connection
		if !conn.KeyIsSet() {
			conn.SetKey(fmt.Sprintf("network:tree/%d/%s/%s", conn.process.Pid, conn.Scope, conn.ID))
			conn.UpdateMeta()
		}
		// save to internal state
		// check if it already exists
		connsLock.Lock()
		_, ok := conns[conn.ID]
		if !ok {
			conns[conn.ID] = conn
		}
		connsLock.Unlock()

	}

	// notify database controller
	dbController.PushUpdate(conn)
}

// delete deletes a link from the storage and propagates the change. Nothing is locked - both the conns map and the connection itself require locking
func (conn *Connection) delete() {
	delete(conns, conn.ID)

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

// SetFirewallHandler sets the firewall handler for this link, and starts a worker to handle the packets.
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
	for {
		pkt := <-conn.pktQueue
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
		conn.Unlock()
		// save does not touch any changing data
		// must not be locked, will deadlock with cleaner functions
		if conn.saveWhenFinished {
			conn.saveWhenFinished = false
			conn.Save()
		}
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
