// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/network/packet"
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
	log.Criticalf("network: link %s does not have a firewallHandler, dropping packet", link)
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
		go link.Save()
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
}

// packetHandler sequentially handles queued packets
func (link *Link) packetHandler() {
	for {
		pkt := <-link.pktQueue
		if pkt == nil {
			return
		}
		link.Lock()
		fwH := link.firewallHandler
		link.Unlock()
		if fwH != nil {
			fwH(pkt, link)
		} else {
			link.ApplyVerdict(pkt)
		}
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

// Save saves the link object in the storage and propagates the change.
func (link *Link) Save() error {
	link.Lock()
	defer link.Unlock()

	if link.comm == nil {
		return errors.New("cannot save link without comms")
	}

	if !link.KeyIsSet() {
		link.SetKey(fmt.Sprintf("network:tree/%d/%s/%s", link.comm.Process().Pid, link.comm.Domain, link.ID))
		link.CreateMeta()
	}

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
	link.Lock()
	defer link.Unlock()

	linksLock.Lock()
	delete(links, link.ID)
	linksLock.Unlock()

	link.Meta().Delete()
	go dbController.PushUpdate(link)
	link.comm.RemoveLink()
	go link.comm.Save()
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
		return link, false
	}
	return CreateLinkFromPacket(pkt), true
}

// CreateLinkFromPacket creates a new Link based on Packet.
func CreateLinkFromPacket(pkt packet.Packet) *Link {
	link := &Link{
		ID:            pkt.GetLinkID(),
		Verdict:       VerdictUndecided,
		Started:       time.Now().Unix(),
		RemoteAddress: pkt.FmtRemoteAddress(),
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
