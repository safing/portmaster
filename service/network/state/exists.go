package state

import (
	"time"

	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

const (
	// UDPConnectionTTL defines the duration after which unseen UDP connections are regarded as ended.
	UDPConnectionTTL = 10 * time.Minute

	// existsMaxTCPTableAge defines the maximum age of the TCP tables used by
	// Exists(). The connection cleaner checks all connections with the same
	// now, so the first check of a cleaner run updates the tables and the
	// other checks of the run reuse them.
	existsMaxTCPTableAge = 1 * time.Second
)

// Exists checks if the given connection is present in the system state tables.
//
// The tables are searched linearly. This is cheap compared to fetching the
// tables: 500 checks against tables with 2000 sockets each take about 0.5ms
// (BenchmarkExistsCleanerRun), so the connection cleaner does not need lookup
// maps for its checks.
func Exists(pktInfo *packet.Info, now time.Time) (exists bool) {
	switch {
	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.TCP:
		return tcp4Table.exists(pktInfo, now)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.TCP:
		return tcp6Table.exists(pktInfo, now)

	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.UDP:
		return udp4Table.exists(pktInfo, now)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.UDP:
		return udp6Table.exists(pktInfo, now)

	default:
		return false
	}
}

// UDPSocketExists checks if the local UDP socket of the given connection is
// still bound, regardless of when the connection was last looked up.
func UDPSocketExists(pktInfo *packet.Info) (exists bool) {
	switch {
	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.UDP:
		return udp4Table.findBindOrDualStack(pktInfo) != nil

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.UDP:
		return udp6Table.findBindOrDualStack(pktInfo) != nil

	default:
		return false
	}
}

func (table *tcpTable) exists(pktInfo *packet.Info, now time.Time) (exists bool) {
	// Update the tables if they are older than the connection that is checked
	// or older than the maximum age. Only lookups of new connections update
	// the tables otherwise, so closed connections would never be noticed on a
	// quiet system.
	updateIfOlderThan := max(
		pktInfo.SeenAt.UnixNano(),
		now.Add(-existsMaxTCPTableAge).UnixNano(),
	)

	if table.existsInTable(pktInfo, updateIfOlderThan) {
		return true
	}

	// Lookups also check the dual-stack table, as IPv4 connections of
	// dual-stack sockets are only listed there (with IPv4-mapped addresses).
	if table.dualStack != nil {
		return table.dualStack.existsInTable(pktInfo, updateIfOlderThan)
	}

	return false
}

func (table *tcpTable) existsInTable(pktInfo *packet.Info, updateIfOlderThan int64) (exists bool) {
	// Update tables if older than the given time (unix nanoseconds).
	// Use the time of the last fetch attempt, so that a failing fetch is not
	// retried for every connection of a cleaner run. The last good tables are
	// used instead until the next cleaner run.
	if table.lastFetchAttemptAt.Load() < updateIfOlderThan {
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
	socketInfo := table.findBindOrDualStack(pktInfo)
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

// findBindOrDualStack finds the socket of the given connection like lookups
// do: first in this table, then in the dual-stack table. Lookups store the
// connection state of sockets of the dual-stack table in this table.
func (table *udpTable) findBindOrDualStack(pktInfo *packet.Info) *socket.BindInfo {
	socketInfo := table.findBind(pktInfo)
	if socketInfo == nil && table.dualStack != nil {
		socketInfo = table.dualStack.findBind(pktInfo)
	}
	return socketInfo
}

// findBind returns the bind of the given connection, matching it the same way
// as lookups do.
func (table *udpTable) findBind(pktInfo *packet.Info) *socket.BindInfo {
	// Update tables if older than the connection that is checked.
	// Use the time of the last fetch attempt, so that a failing fetch is not
	// retried for every connection of a cleaner run.
	if table.lastFetchAttemptAt.Load() < pktInfo.SeenAt.UnixNano() {
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
