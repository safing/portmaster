package network

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/intel"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

// FirewallHandler defines the function signature for a firewall handle function
type FirewallHandler func(pkt packet.Packet, link *Link)

// Link describes a distinct physical connection (e.g. TCP connection) - like an instance - of a Connection.
type Link struct { //nolint:maligned // TODO: fix alignment
	record.Base
	lock sync.Mutex

	ID        string
	Entity    *intel.Entity
	Direction bool

	Verdict          Verdict
	Reason           string
	ReasonID         string // format source[:id[:id]]
	Tunneled         bool
	VerdictPermanent bool
	Inspect          bool
	Started          int64
	Ended            int64

	pktQueue        chan packet.Packet
	firewallHandler FirewallHandler
	comm            *Communication

	activeInspectors []bool
	inspectorData    map[uint8]interface{}
	saveWhenFinished bool
}

// Lock locks the link and the link's Entity.
func (link *Link) Lock() {
	link.lock.Lock()
	link.Entity.Lock()
}

// Lock unlocks the link and the link's Entity.
func (link *Link) Unlock() {
	link.Entity.Unlock()
	link.lock.Unlock()
}

// Communication returns the Communication the Link is part of
func (link *Link) Communication() *Communication {
	link.lock.Lock()
	defer link.lock.Unlock()

	return link.comm
}

// GetVerdict returns the current verdict.
func (link *Link) GetVerdict() Verdict {
	link.lock.Lock()
	defer link.lock.Unlock()

	return link.Verdict
}

// FirewallHandlerIsSet returns whether a firewall handler is set or not
func (link *Link) FirewallHandlerIsSet() bool {
	link.lock.Lock()
	defer link.lock.Unlock()

	return link.firewallHandler != nil
}

// SetFirewallHandler sets the firewall handler for this link
func (link *Link) SetFirewallHandler(handler FirewallHandler) {
	link.lock.Lock()
	defer link.lock.Unlock()

	if link.firewallHandler == nil {
		link.pktQueue = make(chan packet.Packet, 1000)

		// start handling
		module.StartWorker("packet handler", func(ctx context.Context) error {
			link.packetHandler()
			return nil
		})
	}
	link.firewallHandler = handler
}

// StopFirewallHandler unsets the firewall handler
func (link *Link) StopFirewallHandler() {
	link.lock.Lock()
	link.firewallHandler = nil
	link.lock.Unlock()
	link.pktQueue <- nil
}

// HandlePacket queues packet of Link for handling
func (link *Link) HandlePacket(pkt packet.Packet) {
	// get handler
	link.lock.Lock()
	handler := link.firewallHandler
	link.lock.Unlock()

	// send to queue
	if handler != nil {
		link.pktQueue <- pkt
		return
	}

	// no handler!
	log.Warningf("network: link %s does not have a firewallHandler, dropping packet", link)
	err := pkt.Drop()
	if err != nil {
		log.Warningf("network: failed to drop packet %s: %s", pkt, err)
	}
}

// Accept accepts the link and adds the given reason.
func (link *Link) Accept(reason string) {
	link.AddReason(reason)
	link.UpdateVerdict(VerdictAccept)
}

// Deny blocks or drops the link depending on the connection direction and adds the given reason.
func (link *Link) Deny(reason string) {
	if link.Direction {
		link.Drop(reason)
	} else {
		link.Block(reason)
	}
}

// Block blocks the link and adds the given reason.
func (link *Link) Block(reason string) {
	link.AddReason(reason)
	link.UpdateVerdict(VerdictBlock)
}

// Drop drops the link and adds the given reason.
func (link *Link) Drop(reason string) {
	link.AddReason(reason)
	link.UpdateVerdict(VerdictDrop)
}

// RerouteToNameserver reroutes the link to the portmaster nameserver.
func (link *Link) RerouteToNameserver() {
	link.UpdateVerdict(VerdictRerouteToNameserver)
}

// RerouteToTunnel reroutes the link to the tunnel entrypoint and adds the given reason for accepting the connection.
func (link *Link) RerouteToTunnel(reason string) {
	link.AddReason(reason)
	link.UpdateVerdict(VerdictRerouteToTunnel)
}

// UpdateVerdict sets a new verdict for this link, making sure it does not interfere with previous verdicts
func (link *Link) UpdateVerdict(newVerdict Verdict) {
	link.lock.Lock()
	defer link.lock.Unlock()

	if newVerdict > link.Verdict {
		link.Verdict = newVerdict
		link.saveWhenFinished = true
	}
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this link
func (link *Link) AddReason(reason string) {
	if reason == "" {
		return
	}

	link.lock.Lock()
	defer link.lock.Unlock()

	if link.Reason != "" {
		link.Reason += " | "
	}
	link.Reason += reason

	link.saveWhenFinished = true
}

// packetHandler sequentially handles queued packets
func (link *Link) packetHandler() {
	for {
		pkt := <-link.pktQueue
		if pkt == nil {
			return
		}
		// get handler
		link.lock.Lock()
		handler := link.firewallHandler
		link.lock.Unlock()
		// execute handler or verdict
		if handler != nil {
			handler(pkt, link)
		} else {
			link.ApplyVerdict(pkt)
		}
		// submit trace logs
		log.Tracer(pkt.Ctx()).Submit()
	}
}

// ApplyVerdict appies the link verdict to a packet.
func (link *Link) ApplyVerdict(pkt packet.Packet) {
	link.lock.Lock()
	defer link.lock.Unlock()

	var err error

	if link.VerdictPermanent {
		switch link.Verdict {
		case VerdictAccept:
			err = pkt.PermanentAccept()
		case VerdictBlock:
			err = pkt.PermanentBlock()
		case VerdictDrop:
			err = pkt.PermanentDrop()
		case VerdictRerouteToNameserver:
			err = pkt.RerouteToNameserver()
		case VerdictRerouteToTunnel:
			err = pkt.RerouteToTunnel()
		default:
			err = pkt.Drop()
		}
	} else {
		switch link.Verdict {
		case VerdictAccept:
			err = pkt.Accept()
		case VerdictBlock:
			err = pkt.Block()
		case VerdictDrop:
			err = pkt.Drop()
		case VerdictRerouteToNameserver:
			err = pkt.RerouteToNameserver()
		case VerdictRerouteToTunnel:
			err = pkt.RerouteToTunnel()
		default:
			err = pkt.Drop()
		}
	}

	if err != nil {
		log.Warningf("network: failed to apply link verdict to packet %s: %s", pkt, err)
	}
}

// SaveWhenFinished marks the Link for saving after all current actions are finished.
func (link *Link) SaveWhenFinished() {
	// FIXME: check if we should lock here
	link.saveWhenFinished = true
}

// SaveIfNeeded saves the Link if it is marked for saving when finished.
func (link *Link) SaveIfNeeded() {
	link.lock.Lock()
	save := link.saveWhenFinished
	if save {
		link.saveWhenFinished = false
	}
	link.lock.Unlock()

	if save {
		link.saveAndLog()
	}
}

// saveAndLog saves the link object in the storage and propagates the change. It does not return an error, but logs it.
func (link *Link) saveAndLog() {
	err := link.save()
	if err != nil {
		log.Warningf("network: failed to save link %s: %s", link, err)
	}
}

// save saves the link object in the storage and propagates the change.
func (link *Link) save() error {
	// update link
	link.lock.Lock()
	if link.comm == nil {
		link.lock.Unlock()
		return errors.New("cannot save link without comms")
	}

	if !link.KeyIsSet() {
		link.SetKey(fmt.Sprintf("network:tree/%d/%s/%s", link.comm.Process().Pid, link.comm.Scope, link.ID))
		link.UpdateMeta()
	}
	link.saveWhenFinished = false
	link.lock.Unlock()

	// save link
	linksLock.RLock()
	_, ok := links[link.ID]
	linksLock.RUnlock()

	if !ok {
		linksLock.Lock()
		links[link.ID] = link
		linksLock.Unlock()
	}

	go dbController.PushUpdate(link)
	return nil
}

// Delete deletes a link from the storage and propagates the change.
func (link *Link) Delete() {
	linksLock.Lock()
	defer linksLock.Unlock()
	link.lock.Lock()
	defer link.lock.Unlock()

	delete(links, link.ID)

	link.Meta().Delete()
	go dbController.PushUpdate(link)
}

// GetLink fetches a Link from the database from the default namespace for this object
func GetLink(id string) (*Link, bool) {
	linksLock.RLock()
	defer linksLock.RUnlock()

	link, ok := links[id]
	return link, ok
}

// GetOrCreateLinkByPacket returns the associated Link for a packet and a bool expressing if the Link was newly created
func GetOrCreateLinkByPacket(pkt packet.Packet) (*Link, bool) {
	link, ok := GetLink(pkt.GetLinkID())
	if ok {
		log.Tracer(pkt.Ctx()).Tracef("network: assigned to link %s", link.ID)
		return link, false
	}
	link = CreateLinkFromPacket(pkt)
	log.Tracer(pkt.Ctx()).Tracef("network: created new link %s", link.ID)
	return link, true
}

// CreateLinkFromPacket creates a new Link based on Packet.
func CreateLinkFromPacket(pkt packet.Packet) *Link {
	link := &Link{
		ID: pkt.GetLinkID(),
		Entity: (&intel.Entity{
			IP:       pkt.Info().RemoteIP(),
			Protocol: uint8(pkt.Info().Protocol),
			Port:     pkt.Info().RemotePort(),
		}).Init(),
		Direction:        pkt.IsInbound(),
		Verdict:          VerdictUndecided,
		Started:          time.Now().Unix(),
		saveWhenFinished: true,
	}
	return link
}

// GetActiveInspectors returns the list of active inspectors.
func (link *Link) GetActiveInspectors() []bool {
	link.lock.Lock()
	defer link.lock.Unlock()

	return link.activeInspectors
}

// SetActiveInspectors sets the list of active inspectors.
func (link *Link) SetActiveInspectors(new []bool) {
	link.lock.Lock()
	defer link.lock.Unlock()

	link.activeInspectors = new
}

// GetInspectorData returns the list of inspector data.
func (link *Link) GetInspectorData() map[uint8]interface{} {
	link.lock.Lock()
	defer link.lock.Unlock()

	return link.inspectorData
}

// SetInspectorData set the list of inspector data.
func (link *Link) SetInspectorData(new map[uint8]interface{}) {
	link.lock.Lock()
	defer link.lock.Unlock()

	link.inspectorData = new
}

// String returns a string representation of Link.
func (link *Link) String() string {
	link.lock.Lock()
	defer link.lock.Unlock()

	if link.comm == nil {
		return fmt.Sprintf("? <-> %s", link.Entity.IP.String())
	}
	switch link.comm.Scope {
	case IncomingHost, IncomingLAN, IncomingInternet, IncomingInvalid:
		if link.comm.process == nil {
			return fmt.Sprintf("? <- %s", link.Entity.IP.String())
		}
		return fmt.Sprintf("%s <- %s", link.comm.process.String(), link.Entity.IP.String())
	case PeerHost, PeerLAN, PeerInternet, PeerInvalid:
		if link.comm.process == nil {
			return fmt.Sprintf("? -> %s", link.Entity.IP.String())
		}
		return fmt.Sprintf("%s -> %s", link.comm.process.String(), link.Entity.IP.String())
	default:
		if link.comm.process == nil {
			return fmt.Sprintf("? -> %s (%s)", link.comm.Scope, link.Entity.IP.String())
		}
		return fmt.Sprintf("%s to %s (%s)", link.comm.process.String(), link.comm.Scope, link.Entity.IP.String())
	}
}
