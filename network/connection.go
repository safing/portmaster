// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
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

	Domain               string
	Direction            bool
	Intel                *intel.Intel
	process              *process.Process
	Verdict              Verdict
	Reason               string
	Inspect              bool
	FirstLinkEstablished int64
	LastLinkEstablished  int64
}

// Process returns the process that owns the connection.
func (m *Connection) Process() *process.Process {
	return m.process
}

// CantSay sets the connection verdict to "can't say", the connection will be further analysed.
func (m *Connection) CantSay() {
	if m.Verdict != CANTSAY {
		m.Verdict = CANTSAY
		m.Save()
	}
	return
}

// Drop sets the connection verdict to drop.
func (m *Connection) Drop() {
	if m.Verdict != DROP {
		m.Verdict = DROP
		m.Save()
	}
	return
}

// Block sets the connection verdict to block.
func (m *Connection) Block() {
	if m.Verdict != BLOCK {
		m.Verdict = BLOCK
		m.Save()
	}
	return
}

// Accept sets the connection verdict to accept.
func (m *Connection) Accept() {
	if m.Verdict != ACCEPT {
		m.Verdict = ACCEPT
		m.Save()
	}
	return
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this connection
func (m *Connection) AddReason(newReason string) {
	m.Lock()
	defer m.Unlock()

	if m.Reason != "" {
		m.Reason += " | "
	}
	m.Reason += newReason
}

// GetConnectionByFirstPacket returns the matching connection from the internal storage.
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

// GetConnectionByDNSRequest returns the matching connection from the internal storage.
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

// AddLink applies the connection to the link.
func (conn *Connection) AddLink(link *Link) {
	link.Lock()
	defer link.Unlock()
	link.connection = conn
	link.Verdict = conn.Verdict
	link.Inspect = conn.Inspect
	link.Save()

	conn.Lock()
	defer conn.Unlock()
	conn.LastLinkEstablished = time.Now().Unix()
	if conn.FirstLinkEstablished == 0 {
		conn.FirstLinkEstablished = conn.FirstLinkEstablished
	}
	conn.Save()
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
