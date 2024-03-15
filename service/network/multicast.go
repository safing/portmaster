package network

import (
	"net"

	"github.com/safing/portmaster/service/network/netutils"
)

// GetMulticastRequestConn searches for and returns the requesting connnection
// of a possible multicast/broadcast response.
func GetMulticastRequestConn(responseConn *Connection, responseFromNet *net.IPNet) *Connection {
	// Calculate the broadcast address the query would have gone to.
	responseNetBroadcastIP := netutils.GetBroadcastAddress(responseFromNet.IP, responseFromNet.Mask)

	// Find requesting multicast/broadcast connection.
	for _, conn := range conns.clone() {
		switch {
		case !conn.DataIsComplete():
			// Ignore connection with incomplete data.
		case conn.Inbound:
			// Ignore incoming connections.
		case conn.Ended != 0:
			// Ignore ended connections.
		case conn.Entity.Protocol != responseConn.Entity.Protocol:
			// Ignore on protocol mismatch.
		case conn.LocalPort != responseConn.LocalPort:
			// Ignore on local port mismatch.
		case !conn.LocalIP.Equal(responseConn.LocalIP):
			// Ignore on local IP mismatch.
		case !conn.Process().Equal(responseConn.Process()):
			// Ignore if processes mismatch.
		case conn.Entity.IPScope == netutils.LocalMulticast &&
			(responseConn.Entity.IPScope == netutils.LinkLocal ||
				responseConn.Entity.IPScope == netutils.SiteLocal):
			// We found a (possibly routed) multicast request that matches the response!
			return conn
		case conn.Entity.IP.Equal(responseNetBroadcastIP) &&
			responseFromNet.Contains(conn.LocalIP):
			// We found a (link local) broadcast request that matches the response!
			return conn
		}
	}

	return nil
}
