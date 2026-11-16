package state

import (
	"time"

	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

const (
	// UDPConnectionTTL defines the duration after which unseen UDP connections are regarded as ended.
	UDPConnectionTTL = 10 * time.Minute
)

// Exists checks if the given connection is present in the system state tables.
func Exists(pktInfo *packet.Info, now time.Time) (exists bool) {
	// TODO: create lookup maps before running a flurry of Exists() checks.

	switch {
	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.TCP:
		return tcp4Table.exists(pktInfo)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.TCP:
		return tcp6Table.exists(pktInfo)

	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.UDP:
		return udp4Table.exists(pktInfo, now)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.UDP:
		return udp6Table.exists(pktInfo, now)

	default:
		return false
	}
}

func (table *tcpTable) exists(pktInfo *packet.Info) (exists bool) {
	if table.existsInTable(pktInfo) {
		return true
	}

	// Lookups also check the dual-stack table, as IPv4 connections of
	// dual-stack sockets are only listed there (with IPv4-mapped addresses).
	if table.dualStack != nil {
		return table.dualStack.existsInTable(pktInfo)
	}

	return false
}

func (table *tcpTable) existsInTable(pktInfo *packet.Info) (exists bool) {
	// Update tables if older than the connection that is checked.
	if table.lastUpdateAt.Load() < pktInfo.SeenAt.UnixNano() {
		table.updateTables()
	}

	table.lock.RLock()
	defer table.lock.RUnlock()

	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()
	remoteIP := pktInfo.RemoteIP()
	remotePort := pktInfo.RemotePort()

	// search connections
	for _, socketInfo := range table.connections {
		if localPort == socketInfo.Local.Port &&
			remotePort == socketInfo.Remote.Port &&
			remoteIP.Equal(socketInfo.Remote.IP) &&
			localIP.Equal(socketInfo.Local.IP) {
			return true
		}
	}

	return false
}

func (table *udpTable) exists(pktInfo *packet.Info, now time.Time) (exists bool) {
	// Find the socket like lookups do: first in this table, then in the
	// dual-stack table. Lookups store the connection state of sockets of
	// the dual-stack table in this table.
	socketInfo := table.findBind(pktInfo)
	if socketInfo == nil && table.dualStack != nil {
		socketInfo = table.dualStack.findBind(pktInfo)
	}
	if socketInfo == nil {
		return false
	}

	udpConnState, ok := table.getConnState(socketInfo, socket.Address{
		IP:   pktInfo.RemoteIP(),
		Port: pktInfo.RemotePort(),
	})
	if !ok {
		return false
	}

	return udpConnState.lastSeen.After(now.Add(-UDPConnectionTTL))
}

// findBind returns the bind of the given connection, matching it the same way
// as lookups do.
func (table *udpTable) findBind(pktInfo *packet.Info) *socket.BindInfo {
	// Update tables if older than the connection that is checked.
	if table.lastUpdateAt.Load() < pktInfo.SeenAt.UnixNano() {
		table.updateTables()
	}

	table.lock.RLock()
	defer table.lock.RUnlock()

	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()

	// search binds
	for _, socketInfo := range table.binds {
		if localPort == socketInfo.Local.Port &&
			(socketInfo.ListensAny || localIP.Equal(socketInfo.Local.IP)) {
			return socketInfo
		}
	}

	return nil
}
