// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"fmt"
	"sync"
	"time"

	datastore "github.com/ipfs/go-datastore"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/network/packet"
)

type FirewallHandler func(pkt packet.Packet, link *Link)

var (
	linkTimeout  = 10 * time.Minute
	allLinks     = make(map[string]*Link)
	allLinksLock sync.RWMutex
)

// Link describes an distinct physical connection (e.g. TCP connection) - like an instance - of a Connection
type Link struct {
	database.Base
	Verdict          Verdict
	Reason           string
	Tunneled         bool
	VerdictPermanent bool
	Inspect          bool
	Started          int64
	Ended            int64
	connection       *Connection
	RemoteAddress    string
	ActiveInspectors []bool                `json:"-" bson:"-"`
	InspectorData    map[uint8]interface{} `json:"-" bson:"-"`

	pktQueue        chan packet.Packet
	firewallHandler FirewallHandler
}

var linkModel *Link // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(linkModel, func() database.Model { return new(Link) })
}

// Connection returns the Connection the Link is part of
func (m *Link) Connection() *Connection {
	return m.connection
}

// FirewallHandlerIsSet returns whether a firewall handler is set or not
func (m *Link) FirewallHandlerIsSet() bool {
	return m.firewallHandler != nil
}

// SetFirewallHandler sets the firewall handler for this link
func (m *Link) SetFirewallHandler(handler FirewallHandler) {
	if m.firewallHandler == nil {
		m.firewallHandler = handler
		m.pktQueue = make(chan packet.Packet, 1000)
		go m.packetHandler()
		return
	}
	m.firewallHandler = handler
}

// StopFirewallHandler unsets the firewall handler
func (m *Link) StopFirewallHandler() {
	m.pktQueue <- nil
}

// HandlePacket queues packet of Link for handling
func (m *Link) HandlePacket(pkt packet.Packet) {
	if m.firewallHandler != nil {
		m.pktQueue <- pkt
		return
	}
	log.Criticalf("network: link %s does not have a firewallHandler, maybe its a copy, dropping packet", m)
	pkt.Drop()
}

// UpdateVerdict sets a new verdict for this link, making sure it does not interfere with previous verdicts
func (m *Link) UpdateVerdict(newVerdict Verdict) {
	if newVerdict > m.Verdict {
		m.Verdict = newVerdict
		m.Save()
	}
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this link
func (m *Link) AddReason(newReason string) {
	if m.Reason != "" {
		m.Reason += " | "
	}
	m.Reason += newReason
}

// packetHandler sequentially handles queued packets
func (m *Link) packetHandler() {
	for {
		pkt := <-m.pktQueue
		if pkt == nil {
			break
		}
		m.firewallHandler(pkt, m)
	}
	m.firewallHandler = nil
}

// Create creates a new database entry in the database in the default namespace for this object
func (m *Link) Create(name string) error {
	m.CreateShallow(name)
	return m.CreateObject(&database.OrphanedLink, name, m)
}

// Create creates a new database entry in the database in the default namespace for this object
func (m *Link) CreateShallow(name string) {
	allLinksLock.Lock()
	allLinks[name] = m
	allLinksLock.Unlock()
}

// CreateWithDefaultKey creates a new database entry in the database in the default namespace for this object using the default key
func (m *Link) CreateInConnectionNamespace(name string) error {
	if m.connection != nil {
		return m.CreateObject(m.connection.GetKey(), name, m)
	}
	return m.CreateObject(&database.OrphanedLink, name, m)
}

// Save saves the object to the database (It must have been either already created or loaded from the database)
func (m *Link) Save() error {
	return m.SaveObject(m)
}

// GetLink fetches a Link from the database from the default namespace for this object
func GetLink(name string) (*Link, error) {
	allLinksLock.RLock()
	link, ok := allLinks[name]
	allLinksLock.RUnlock()
	if !ok {
		return nil, database.ErrNotFound
	}
	return link, nil
	// return GetLinkFromNamespace(&database.RunningLink, name)
}

func SaveInCache(link *Link) {

}

// GetLinkFromNamespace fetches a Link form the database, but from a custom namespace
func GetLinkFromNamespace(namespace *datastore.Key, name string) (*Link, error) {
	object, err := database.GetAndEnsureModel(namespace, name, linkModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*Link)
	if !ok {
		return nil, database.NewMismatchError(object, linkModel)
	}
	return model, nil
}

// GetOrCreateLinkByPacket returns the associated Link for a packet and a bool expressing if the Link was newly created
func GetOrCreateLinkByPacket(pkt packet.Packet) (*Link, bool) {
	link, err := GetLink(pkt.GetConnectionID())
	if err != nil {
		return CreateLinkFromPacket(pkt), true
	}
	return link, false
}

// CreateLinkFromPacket creates a new Link based on Packet. The Link is shallowly saved and SHOULD be saved to the database as soon more information is available
func CreateLinkFromPacket(pkt packet.Packet) *Link {
	link := &Link{
		Verdict:       UNDECIDED,
		Started:       time.Now().Unix(),
		RemoteAddress: pkt.FmtRemoteAddress(),
	}
	link.CreateShallow(pkt.GetConnectionID())
	return link
}

// FORMATTING
func (m *Link) String() string {
	if m.connection == nil {
		return fmt.Sprintf("? <-> %s", m.RemoteAddress)
	}
	switch m.connection.Domain {
	case "I":
		if m.connection.process == nil {
			return fmt.Sprintf("? <- %s", m.RemoteAddress)
		}
		return fmt.Sprintf("%s <- %s", m.connection.process.String(), m.RemoteAddress)
	case "D":
		if m.connection.process == nil {
			return fmt.Sprintf("? -> %s", m.RemoteAddress)
		}
		return fmt.Sprintf("%s -> %s", m.connection.process.String(), m.RemoteAddress)
	default:
		if m.connection.process == nil {
			return fmt.Sprintf("? -> %s (%s)", m.connection.Domain, m.RemoteAddress)
		}
		return fmt.Sprintf("%s to %s (%s)", m.connection.process.String(), m.connection.Domain, m.RemoteAddress)
	}
}
