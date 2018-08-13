// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"fmt"
	"net"
	"time"

	"github.com/Safing/safing-core/database"
	"github.com/Safing/safing-core/intel"
	"github.com/Safing/safing-core/network/packet"
	"github.com/Safing/safing-core/process"

	datastore "github.com/ipfs/go-datastore"
)

// Connection describes a connection between a process and a domain
type Connection struct {
	database.Base
	Domain               string
	Direction            bool
	Intel                *intel.Intel
	process              *process.Process
	Verdict              Verdict
	Reason               string
	Inspect              bool
	FirstLinkEstablished int64
}

var connectionModel *Connection // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(connectionModel, func() database.Model { return new(Connection) })
}

func (m *Connection) Process() *process.Process {
	return m.process
}

// Create creates a new database entry in the database in the default namespace for this object
func (m *Connection) Create(name string) error {
	return m.CreateObject(&database.OrphanedConnection, name, m)
}

// CreateInProcessNamespace creates a new database entry in the namespace of the connection's process
func (m *Connection) CreateInProcessNamespace() error {
	if m.process != nil {
		return m.CreateObject(m.process.GetKey(), m.Domain, m)
	}
	return m.CreateObject(&database.OrphanedConnection, m.Domain, m)
}

// Save saves the object to the database (It must have been either already created or loaded from the database)
func (m *Connection) Save() error {
	return m.SaveObject(m)
}

func (m *Connection) CantSay() {
	if m.Verdict != CANTSAY {
		m.Verdict = CANTSAY
		m.SaveObject(m)
	}
	return
}

func (m *Connection) Drop() {
	if m.Verdict != DROP {
		m.Verdict = DROP
		m.SaveObject(m)
	}
	return
}

func (m *Connection) Block() {
	if m.Verdict != BLOCK {
		m.Verdict = BLOCK
		m.SaveObject(m)
	}
	return
}

func (m *Connection) Accept() {
	if m.Verdict != ACCEPT {
		m.Verdict = ACCEPT
		m.SaveObject(m)
	}
	return
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this connection
func (m *Connection) AddReason(newReason string) {
	if m.Reason != "" {
		m.Reason += " | "
	}
	m.Reason += newReason
}

func GetConnectionByFirstPacket(pkt packet.Packet) (*Connection, error) {
	// get Process
	proc, direction, err := process.GetProcessByPacket(pkt)
	if err != nil {
		return nil, err
	}

	// if INBOUND
	if direction {
		connection, err := GetConnectionFromProcessNamespace(proc, "I")
		if err != nil {
			connection = &Connection{
				Domain:               "I",
				Direction:            true,
				process:              proc,
				Inspect:              true,
				FirstLinkEstablished: time.Now().Unix(),
			}
		}
		return connection, nil
	}

	// get domain
	ipinfo, err := intel.GetIPInfo(pkt.FmtRemoteIP())
	if err != nil {
		// if no domain could be found, it must be a direct connection
		connection, err := GetConnectionFromProcessNamespace(proc, "D")
		if err != nil {
			connection = &Connection{
				Domain:               "D",
				process:              proc,
				Inspect:              true,
				FirstLinkEstablished: time.Now().Unix(),
			}
		}
		return connection, nil
	}

	// FIXME: how to handle multiple possible domains?
	connection, err := GetConnectionFromProcessNamespace(proc, ipinfo.Domains[0])
	if err != nil {
		connection = &Connection{
			Domain:               ipinfo.Domains[0],
			process:              proc,
			Inspect:              true,
			FirstLinkEstablished: time.Now().Unix(),
		}
	}
	return connection, nil
}

// var localhost = net.IPv4(127, 0, 0, 1)

var (
	dnsAddress        = net.IPv4(127, 0, 0, 1)
	dnsPort    uint16 = 53
)

func GetConnectionByDNSRequest(ip net.IP, port uint16, fqdn string) (*Connection, error) {
	// get Process
	proc, err := process.GetProcessByEndpoints(ip, port, dnsAddress, dnsPort, packet.UDP)
	if err != nil {
		return nil, err
	}

	connection, err := GetConnectionFromProcessNamespace(proc, fqdn)
	if err != nil {
		connection = &Connection{
			Domain:  fqdn,
			process: proc,
			Inspect: true,
		}
		connection.CreateInProcessNamespace()
	}
	return connection, nil
}

// GetConnection fetches a Connection from the database from the default namespace for this object
func GetConnection(name string) (*Connection, error) {
	return GetConnectionFromNamespace(&database.OrphanedConnection, name)
}

// GetConnectionFromProcessNamespace fetches a Connection from the namespace of its process
func GetConnectionFromProcessNamespace(process *process.Process, domain string) (*Connection, error) {
	return GetConnectionFromNamespace(process.GetKey(), domain)
}

// GetConnectionFromNamespace fetches a Connection form the database, but from a custom namespace
func GetConnectionFromNamespace(namespace *datastore.Key, name string) (*Connection, error) {
	object, err := database.GetAndEnsureModel(namespace, name, connectionModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*Connection)
	if !ok {
		return nil, database.NewMismatchError(object, connectionModel)
	}
	return model, nil
}

func (m *Connection) AddLink(link *Link, pkt packet.Packet) {
	link.connection = m
	link.Verdict = m.Verdict
	link.Inspect = m.Inspect
	if m.FirstLinkEstablished == 0 {
		m.FirstLinkEstablished = time.Now().Unix()
		m.Save()
	}
	link.CreateInConnectionNamespace(pkt.GetConnectionID())
}

// FORMATTING

func (m *Connection) String() string {
	switch m.Domain {
	case "I":
		if m.process == nil {
			return "? <- *"
		}
		return fmt.Sprintf("%s <- *", m.process.String())
	case "D":
		if m.process == nil {
			return "? -> *"
		}
		return fmt.Sprintf("%s -> *", m.process.String())
	default:
		if m.process == nil {
			return fmt.Sprintf("? -> %s", m.Domain)
		}
		return fmt.Sprintf("%s -> %s", m.process.String(), m.Domain)
	}
}
