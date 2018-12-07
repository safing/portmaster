// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portmaster/intel"
	"github.com/Safing/portmaster/network/packet"
	"github.com/Safing/portmaster/process"
)

// Connection describes a connection between a process and a domain
type Connection struct {
	record.Base
	sync.Mutex

	Domain    string
	Direction bool
	Intel     *intel.Intel
	process   *process.Process
	Verdict   Verdict
	Reason    string
	Inspect   bool

	FirstLinkEstablished int64
	LastLinkEstablished  int64
	LinkCount            uint
}

// Process returns the process that owns the connection.
func (conn *Connection) Process() *process.Process {
	return conn.process
}

// Accept accepts the connection and adds the given reason.
func (conn *Link) Accept(reason string) {
	conn.AddReason(reason)
	conn.UpdateVerdict(ACCEPT)
}

// Deny blocks or drops the connection depending on the connection direction and adds the given reason.
func (conn *Link) Deny(reason string) {
	if conn.Direction {
		conn.Drop(reason)
	} else {
		conn.Block(reason)
	}
}

// Block blocks the connection and adds the given reason.
func (conn *Link) Block(reason string) {
	conn.AddReason(reason)
	conn.UpdateVerdict(BLOCK)
}

// Drop drops the connection and adds the given reason.
func (conn *Link) Drop(reason string) {
	conn.AddReason(reason)
	conn.UpdateVerdict(DROP)
}

// UpdateVerdict sets a new verdict for this link, making sure it does not interfere with previous verdicts
func (conn *Connection) UpdateVerdict(newVerdict Verdict) {
	conn.Lock()
	defer conn.Unlock()

	if newVerdict > conn.Verdict {
		conn.Verdict = newVerdict
		conn.Save()
	}
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this connection
func (conn *Connection) AddReason(reason string) {
	if reason == "" {
		return
	}

	conn.Lock()
	defer conn.Unlock()

	if conn.Reason != "" {
		conn.Reason += " | "
	}
	conn.Reason += reason
}

// GetConnectionByFirstPacket returns the matching connection from the internal storage.
func GetConnectionByFirstPacket(pkt packet.Packet) (*Connection, error) {
	// get Process
	proc, direction, err := process.GetProcessByPacket(pkt)
	if err != nil {
		return nil, err
	}
	var domain string

	// Incoming
	if direction {
		switch netutils.ClassifyIP(pkt.GetIPHeader().Src) {
		case HostLocal:
			domain = IncomingHost
		case LinkLocal, SiteLocal, LocalMulticast:
			domain = IncomingLAN
		case Global, GlobalMulticast:
			domain = IncomingInternet
		case Invalid:
			domain = IncomingInvalid
		}

		connection, ok := GetConnection(proc.Pid, domain)
		if !ok {
			connection = &Connection{
				Domain:               domain,
				Direction:            Inbound,
				process:              proc,
				Inspect:              true,
				FirstLinkEstablished: time.Now().Unix(),
			}
		}
		connection.process.AddConnection()
		return connection, nil
	}

	// get domain
	ipinfo, err := intel.GetIPInfo(pkt.FmtRemoteIP())

	// PeerToPeer
	if err != nil {
		// if no domain could be found, it must be a direct connection

		switch netutils.ClassifyIP(pkt.GetIPHeader().Dst) {
		case HostLocal:
			domain = PeerHost
		case LinkLocal, SiteLocal, LocalMulticast:
			domain = PeerLAN
		case Global, GlobalMulticast:
			domain = PeerInternet
		case Invalid:
			domain = PeerInvalid
		}

		connection, ok := GetConnection(proc.Pid, domain)
		if !ok {
			connection = &Connection{
				Domain:               domain,
				Direction:            Outbound,
				process:              proc,
				Inspect:              true,
				FirstLinkEstablished: time.Now().Unix(),
			}
		}
		connection.process.AddConnection()
		return connection, nil
	}

	// To Domain
	// FIXME: how to handle multiple possible domains?
	connection, ok := GetConnection(proc.Pid, ipinfo.Domains[0])
	if !ok {
		connection = &Connection{
			Domain:               ipinfo.Domains[0],
			Direction:            Outbound,
			process:              proc,
			Inspect:              true,
			FirstLinkEstablished: time.Now().Unix(),
		}
	}
	connection.process.AddConnection()
	return connection, nil
}

// var localhost = net.IPv4(127, 0, 0, 1)

var (
	dnsAddress        = net.IPv4(127, 0, 0, 1)
	dnsPort    uint16 = 53
)

// GetConnectionByDNSRequest returns the matching connection from the internal storage.
func GetConnectionByDNSRequest(ip net.IP, port uint16, fqdn string) (*Connection, error) {
	// get Process
	proc, err := process.GetProcessByEndpoints(ip, port, dnsAddress, dnsPort, packet.UDP)
	if err != nil {
		return nil, err
	}

	connection, ok := GetConnection(proc.Pid, fqdn)
	if !ok {
		connection = &Connection{
			Domain:  fqdn,
			process: proc,
			Inspect: true,
		}
		connection.process.AddConnection()
		connection.Save()
	}
	return connection, nil
}

// GetConnection fetches a connection object from the internal storage.
func GetConnection(pid int, domain string) (conn *Connection, ok bool) {
	dataLock.RLock()
	defer dataLock.RUnlock()
	conn, ok = connections[fmt.Sprintf("%d/%s", pid, domain)]
	return
}

func (conn *Connection) makeKey() string {
	return fmt.Sprintf("%d/%s", conn.process.Pid, conn.Domain)
}

// Save saves the connection object in the storage and propagates the change.
func (conn *Connection) Save() error {
	if conn.process == nil {
		return errors.New("cannot save connection without process")
	}

	if conn.DatabaseKey() == "" {
		conn.SetKey(fmt.Sprintf("network:tree/%d/%s", conn.process.Pid, conn.Domain))
		conn.CreateMeta()
	}

	key := conn.makeKey()
	dataLock.RLock()
	_, ok := connections[key]
	dataLock.RUnlock()

	if !ok {
		dataLock.Lock()
		connections[key] = conn
		dataLock.Unlock()
	}

	dbController.PushUpdate(conn)
	return nil
}

// Delete deletes a connection from the storage and propagates the change.
func (conn *Connection) Delete() {
	dataLock.Lock()
	defer dataLock.Unlock()
	delete(connections, conn.makeKey())
	conn.Lock()
	defer conn.Lock()
	conn.Meta().Delete()
	dbController.PushUpdate(conn)
	conn.process.RemoveConnection()
}

// AddLink applies the connection to the link and increases sets counter and timestamps.
func (conn *Connection) AddLink(link *Link) {
	link.Lock()
	defer link.Unlock()
	link.connection = conn
	link.Verdict = conn.Verdict
	link.Inspect = conn.Inspect
	link.Save()

	conn.Lock()
	defer conn.Unlock()
	conn.LinkCount++
	conn.LastLinkEstablished = time.Now().Unix()
	if conn.FirstLinkEstablished == 0 {
		conn.FirstLinkEstablished = conn.LastLinkEstablished
	}
	conn.Save()
}

// RemoveLink lowers the link counter by one.
func (conn *Connection) RemoveLink() {
	conn.Lock()
	defer conn.Unlock()
	if conn.LinkCount > 0 {
		conn.LinkCount--
	}
}

// String returns a string representation of Connection.
func (conn *Connection) String() string {
	switch conn.Domain {
	case "I":
		if conn.process == nil {
			return "? <- *"
		}
		return fmt.Sprintf("%s <- *", conn.process.String())
	case "D":
		if conn.process == nil {
			return "? -> *"
		}
		return fmt.Sprintf("%s -> *", conn.process.String())
	default:
		if conn.process == nil {
			return fmt.Sprintf("? -> %s", conn.Domain)
		}
		return fmt.Sprintf("%s -> %s", conn.process.String(), conn.Domain)
	}
}
