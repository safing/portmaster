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
	connection      *Connection

	activeInspectors []bool
	inspectorData    map[uint8]interface{}
}

// Connection returns the Connection the Link is part of
func (link *Link) Connection() *Connection {
	return link.connection
}

// FirewallHandlerIsSet returns whether a firewall handler is set or not
func (link *Link) FirewallHandlerIsSet() bool {
	return link.firewallHandler != nil
}

// SetFirewallHandler sets the firewall handler for this link
func (link *Link) SetFirewallHandler(handler FirewallHandler) {
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
	link.pktQueue <- nil
}

// HandlePacket queues packet of Link for handling
func (link *Link) HandlePacket(pkt packet.Packet) {
	if link.firewallHandler != nil {
		link.pktQueue <- pkt
		return
	}
	log.Criticalf("network: link %s does not have a firewallHandler (maybe it's a copy), dropping packet", link)
	pkt.Drop()
}

// UpdateVerdict sets a new verdict for this link, making sure it does not interfere with previous verdicts
func (link *Link) UpdateVerdict(newVerdict Verdict) {
	if newVerdict > link.Verdict {
		link.Verdict = newVerdict
		link.Save()
	}
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this link
func (link *Link) AddReason(newReason string) {
	link.Lock()
	defer link.Unlock()

	if link.Reason != "" {
		link.Reason += " | "
	}
	link.Reason += newReason
}

// packetHandler sequentially handles queued packets
func (link *Link) packetHandler() {
	for {
		pkt := <-link.pktQueue
		if pkt == nil {
			break
		}
		link.firewallHandler(pkt, link)
	}
	link.firewallHandler = nil
}

// Save saves the link object in the storage and propagates the change.
func (link *Link) Save() error {
	if link.connection == nil {
		return errors.New("cannot save link without connection")
	}

	if link.DatabaseKey() == "" {
		link.SetKey(fmt.Sprintf("network:tree/%d/%s/%s", link.connection.Process().Pid, link.connection.Domain, link.ID))
		link.CreateMeta()
	}

	dataLock.RLock()
	_, ok := links[link.ID]
	dataLock.RUnlock()

	if !ok {
		dataLock.Lock()
		links[link.ID] = link
		dataLock.Unlock()
	}

	dbController.PushUpdate(link)
	return nil
}

// Delete deletes a link from the storage and propagates the change.
func (link *Link) Delete() {
	dataLock.Lock()
	defer dataLock.Unlock()
	delete(links, link.ID)
	link.Lock()
	defer link.Lock()
	link.Meta().Delete()
	dbController.PushUpdate(link)
	link.connection.RemoveLink()
}

// GetLink fetches a Link from the database from the default namespace for this object
func GetLink(id string) (*Link, bool) {
	dataLock.RLock()
	defer dataLock.RUnlock()

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
		Verdict:       UNDECIDED,
		Started:       time.Now().Unix(),
		RemoteAddress: pkt.FmtRemoteAddress(),
	}
	return link
}

// String returns a string representation of Link.
func (link *Link) String() string {
	if link.connection == nil {
		return fmt.Sprintf("? <-> %s", link.RemoteAddress)
	}
	switch link.connection.Domain {
	case "I":
		if link.connection.process == nil {
			return fmt.Sprintf("? <- %s", link.RemoteAddress)
		}
		return fmt.Sprintf("%s <- %s", link.connection.process.String(), link.RemoteAddress)
	case "D":
		if link.connection.process == nil {
			return fmt.Sprintf("? -> %s", link.RemoteAddress)
		}
		return fmt.Sprintf("%s -> %s", link.connection.process.String(), link.RemoteAddress)
	default:
		if link.connection.process == nil {
			return fmt.Sprintf("? -> %s (%s)", link.connection.Domain, link.RemoteAddress)
		}
		return fmt.Sprintf("%s to %s (%s)", link.connection.process.String(), link.connection.Domain, link.RemoteAddress)
	}
}
