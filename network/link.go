package network

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

// FirewallHandler defines the function signature for a firewall handle function
type FirewallHandler func(pkt packet.Packet, link *Link)

var (
	linkTimeout = 10 * time.Minute
)

// Link describes a distinct physical connection (e.g. TCP connection) - like an instance - of a Connection.
type Link struct {
	record.Base
	sync.Mutex

	ID string

	Verdict          Verdict
	Reason           string
	Tunneled         bool
	VerdictPermanent bool
	Inspect          bool
	Started          int64
	Ended            int64
	RemoteAddress    string

	pktQueue        chan packet.Packet
	firewallHandler FirewallHandler
	comm            *Communication

	activeInspectors []bool
	inspectorData    map[uint8]interface{}
	saveWhenFinished bool
}

// Communication returns the Communication the Link is part of
func (link *Link) Communication() *Communication {
	link.Lock()
	defer link.Unlock()

	return link.comm
}

// GetVerdict returns the current verdict.
func (link *Link) GetVerdict() Verdict {
	link.Lock()
	defer link.Unlock()

	return link.Verdict
}

// FirewallHandlerIsSet returns whether a firewall handler is set or not
func (link *Link) FirewallHandlerIsSet() bool {
	link.Lock()
	defer link.Unlock()

	return link.firewallHandler != nil
}

// SetFirewallHandler sets the firewall handler for this link
func (link *Link) SetFirewallHandler(handler FirewallHandler) {
	link.Lock()
	defer link.Unlock()

	if link.firewallHandler == nil {
		link.firewallHandler = handler
		link.pktQueue = make(chan packet.Packet, 1000)
		go link.packetHandler()
		return
	}
	link.firewallHandler = handler
}

// StopFirewallHandler unsets the firewall handler
func (link *Link) StopFirewallHandler() {
	link.Lock()
	link.firewallHandler = nil
	link.Unlock()
	link.pktQueue <- nil
}

// HandlePacket queues packet of Link for handling
func (link *Link) HandlePacket(pkt packet.Packet) {
	link.Lock()
	defer link.Unlock()

	if link.firewallHandler != nil {
		link.pktQueue <- pkt
		return
	}
	log.Warningf("network: link %s does not have a firewallHandler, dropping packet", link)
	pkt.Drop()
}

// Accept accepts the link and adds the given reason.
func (link *Link) Accept(reason string) {
	link.AddReason(reason)
	link.UpdateVerdict(VerdictAccept)
}

// Deny blocks or drops the link depending on the connection direction and adds the given reason.
func (link *Link) Deny(reason string) {
	if link.comm != nil && link.comm.Direction {
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
	link.Lock()
	defer link.Unlock()

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

	link.Lock()
	defer link.Unlock()

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
		link.Lock()
		handler := link.firewallHandler
		link.Unlock()
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
	link.Lock()
	defer link.Unlock()

	if link.VerdictPermanent {
		switch link.Verdict {
		case VerdictAccept:
			pkt.PermanentAccept()
		case VerdictBlock:
			pkt.PermanentBlock()
		case VerdictDrop:
			pkt.PermanentDrop()
		case VerdictRerouteToNameserver:
			pkt.RerouteToNameserver()
		case VerdictRerouteToTunnel:
			pkt.RerouteToTunnel()
		default:
			pkt.Drop()
		}
	} else {
		switch link.Verdict {
		case VerdictAccept:
			pkt.Accept()
		case VerdictBlock:
			pkt.Block()
		case VerdictDrop:
			pkt.Drop()
		case VerdictRerouteToNameserver:
			pkt.RerouteToNameserver()
		case VerdictRerouteToTunnel:
			pkt.RerouteToTunnel()
		default:
			pkt.Drop()
		}
	}
}

// SaveWhenFinished marks the Link for saving after all current actions are finished.
func (link *Link) SaveWhenFinished() {
	link.saveWhenFinished = true
}

// SaveIfNeeded saves the Link if it is marked for saving when finished.
func (link *Link) SaveIfNeeded() {
	link.Lock()
	save := link.saveWhenFinished
	if save {
		link.saveWhenFinished = false
	}
	link.Unlock()

	if save {
		link.save()
	}
}

// Save saves the link object in the storage and propagates the change.
func (link *Link) save() error {
	// update link
	link.Lock()
	if link.comm == nil {
		link.Unlock()
		return errors.New("cannot save link without comms")
	}

	if !link.KeyIsSet() {
		link.SetKey(fmt.Sprintf("network:tree/%d/%s/%s", link.comm.Process().Pid, link.comm.Domain, link.ID))
		link.UpdateMeta()
	}
	link.saveWhenFinished = false
	link.Unlock()

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
	link.Lock()
	defer link.Unlock()

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
		ID:               pkt.GetLinkID(),
		Verdict:          VerdictUndecided,
		Started:          time.Now().Unix(),
		RemoteAddress:    pkt.FmtRemoteAddress(),
		saveWhenFinished: true,
	}
	return link
}

// GetActiveInspectors returns the list of active inspectors.
func (link *Link) GetActiveInspectors() []bool {
	link.Lock()
	defer link.Unlock()

	return link.activeInspectors
}

// SetActiveInspectors sets the list of active inspectors.
func (link *Link) SetActiveInspectors(new []bool) {
	link.Lock()
	defer link.Unlock()

	link.activeInspectors = new
}

// GetInspectorData returns the list of inspector data.
func (link *Link) GetInspectorData() map[uint8]interface{} {
	link.Lock()
	defer link.Unlock()

	return link.inspectorData
}

// SetInspectorData set the list of inspector data.
func (link *Link) SetInspectorData(new map[uint8]interface{}) {
	link.Lock()
	defer link.Unlock()

	link.inspectorData = new
}

// String returns a string representation of Link.
func (link *Link) String() string {
	link.Lock()
	defer link.Unlock()

	if link.comm == nil {
		return fmt.Sprintf("? <-> %s", link.RemoteAddress)
	}
	switch link.comm.Domain {
	case "I":
		if link.comm.process == nil {
			return fmt.Sprintf("? <- %s", link.RemoteAddress)
		}
		return fmt.Sprintf("%s <- %s", link.comm.process.String(), link.RemoteAddress)
	case "D":
		if link.comm.process == nil {
			return fmt.Sprintf("? -> %s", link.RemoteAddress)
		}
		return fmt.Sprintf("%s -> %s", link.comm.process.String(), link.RemoteAddress)
	default:
		if link.comm.process == nil {
			return fmt.Sprintf("? -> %s (%s)", link.comm.Domain, link.RemoteAddress)
		}
		return fmt.Sprintf("%s to %s (%s)", link.comm.process.String(), link.comm.Domain, link.RemoteAddress)
	}
}
