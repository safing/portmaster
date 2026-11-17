package state

import (
	"errors"

	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

// - TCP
//   - Outbound: Match listeners (in!), then connections (out!)
//   - Inbound: Match listeners (in!), then connections (out!)
//   - Clean via connections
// - UDP
//   - Any connection: match specific local address or zero IP
//   - In or out: save direction of first packet:
//     - map[<local udp bind ip+port>]map[<remote ip+port>]{direction, lastSeen}
//       - only clean if <local udp bind ip+port> is removed by OS
//       - limit <remote ip+port> to 256 entries?
//       - clean <remote ip+port> after 72hrs?
//       - switch direction to outbound if outbound packet is seen?
// - IP: Unidentified Process

// Errors.
var (
	ErrConnectionNotFound = errors.New("could not find connection in system state tables")
	ErrPIDNotFound        = errors.New("could not find pid for socket inode")
)

// The first try of a lookup always uses the current tables, whatever their age:
//   - If they were fetched before the packet was seen, they usually still hold
//     the socket (listeners, UDP sockets and connections that were looked up
//     before), which saves fetching the tables for most packets. If they do
//     not, the next try fetches them.
//   - If they were fetched after the packet was seen (by the lookup of another
//     packet while this one was waiting), they are as good as newly fetched
//     ones. Fetching them again anyway made every packet of a burst of new
//     connections wait for the pause between table updates.
//
// Note that the first case trusts tables of any age: a socket that was closed
// since, and whose local port (UDP, listeners) or address pair (TCP) is in use
// by another process now, is found first. Exists() does not use old tables.
const (
	lookupTries     = 5
	fastLookupTries = 2 // 1. current table, 2. get table with max 10ms, could be 0ms, 3. 10ms wait
)

// Lookup looks for the given connection in the system state tables and returns the PID of the associated process and whether the connection is inbound.
func Lookup(pktInfo *packet.Info, fast bool) (pid int, inbound bool, err error) {
	// auto-detect version
	if pktInfo.Version == 0 {
		if ip := pktInfo.LocalIP().To4(); ip != nil {
			pktInfo.Version = packet.IPv4
		} else {
			pktInfo.Version = packet.IPv6
		}
	}

	switch {
	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.TCP:
		return tcp4Table.lookup(pktInfo, fast)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.TCP:
		return tcp6Table.lookup(pktInfo, fast)

	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.UDP:
		return udp4Table.lookup(pktInfo, fast)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.UDP:
		return udp6Table.lookup(pktInfo, fast)

	default:
		return socket.UndefinedProcessID, pktInfo.Inbound, errors.New("unsupported protocol for finding process")
	}
}

func (table *tcpTable) lookup(pktInfo *packet.Info, fast bool) (
	pid int,
	inbound bool,
	err error,
) {
	// Prepare variables.
	var (
		connections []*socket.ConnectionInfo
		listeners   []*socket.BindInfo

		dualStackConnections []*socket.ConnectionInfo
		dualStackListeners   []*socket.BindInfo
	)

	// Search for the socket until found.
	for i := 1; i <= lookupTries; i++ {
		// Use the existing tables for the first check, see lookupTries.
		if i == 1 {
			connections, listeners = table.getCurrentTables()
		} else {
			connections, listeners = table.updateTables()
		}

		// Check tables for socket.
		socketInfo, inbound := findTCPSocket(pktInfo, connections, listeners)

		// If there's a match, check if we have the PID and return.
		if socketInfo != nil {
			return CheckPID(socketInfo, inbound)
		}

		// DUAL-STACK

		// Skip if dualStack is not enabled.
		if table.dualStack == nil {
			continue
		}

		// Use the existing tables for the first check, see lookupTries.
		if i == 1 {
			dualStackConnections, dualStackListeners = table.dualStack.getCurrentTables()
		} else {
			dualStackConnections, dualStackListeners = table.dualStack.updateTables()
		}

		// Check tables for socket.
		socketInfo, inbound = findTCPSocket(pktInfo, dualStackConnections, dualStackListeners)

		// If there's a match, check if we have the PID and return.
		if socketInfo != nil {
			return CheckPID(socketInfo, inbound)
		}

		// Search less if we want to be fast.
		if fast && i >= fastLookupTries {
			break
		}
	}

	return socket.UndefinedProcessID, pktInfo.Inbound, ErrConnectionNotFound
}

func findTCPSocket(
	pktInfo *packet.Info,
	connections []*socket.ConnectionInfo,
	listeners []*socket.BindInfo,
) (
	socketInfo socket.Info,
	inbound bool,
) {
	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()

	// always search listeners first
	for _, socketInfo := range listeners {
		if localPort == socketInfo.Local.Port &&
			(socketInfo.ListensAny || localIP.Equal(socketInfo.Local.IP)) {
			return socketInfo, true
		}
	}

	remoteIP := pktInfo.RemoteIP()
	remotePort := pktInfo.RemotePort()

	// search connections
	for _, socketInfo := range connections {
		if localPort == socketInfo.Local.Port &&
			remotePort == socketInfo.Remote.Port &&
			remoteIP.Equal(socketInfo.Remote.IP) &&
			localIP.Equal(socketInfo.Local.IP) {
			return socketInfo, false
		}
	}

	return nil, false
}

func (table *udpTable) lookup(pktInfo *packet.Info, fast bool) (
	pid int,
	inbound bool,
	err error,
) {
	// TODO: Currently broadcast/multicast scopes are not checked, so we might
	// attribute an incoming broadcast/multicast packet to the wrong process if
	// there are multiple processes listening on the same local port, but
	// binding to different addresses. This highly unusual for clients.
	isInboundMulticast := pktInfo.Inbound && netutils.GetIPScope(pktInfo.LocalIP()) == netutils.LocalMulticast

	// Prepare variables.
	var (
		binds          []*socket.BindInfo
		dualStackBinds []*socket.BindInfo
	)

	// Search for the socket until found.
	for i := 1; i <= lookupTries; i++ {
		// Use the existing tables for the first check, see lookupTries.
		if i == 1 {
			binds = table.getCurrentTables()
		} else {
			binds = table.updateTables()
		}

		// Check tables for socket.
		socketInfo := findUDPSocket(pktInfo, binds, isInboundMulticast)

		// If there's a match, do some last checks and return.
		if socketInfo != nil {
			// If there is no remote port, do check for the direction of the
			// connection. This will be the case for pure checking functions
			// that do not want to change direction state.
			if pktInfo.RemotePort() == 0 {
				return CheckPID(socketInfo, pktInfo.Inbound)
			}

			// Get (and save) the direction of the connection.
			connInbound := table.getDirection(socketInfo, pktInfo)

			// Check we have the PID and return.
			return CheckPID(socketInfo, connInbound)
		}

		// DUAL-STACK

		// Skip if dualStack is not enabled.
		if table.dualStack == nil {
			continue
		}

		// Use the existing tables for the first check, see lookupTries.
		if i == 1 {
			dualStackBinds = table.dualStack.getCurrentTables()
		} else {
			dualStackBinds = table.dualStack.updateTables()
		}

		// Check tables for socket.
		socketInfo = findUDPSocket(pktInfo, dualStackBinds, isInboundMulticast)

		// If there's a match, do some last checks and return.
		if socketInfo != nil {
			// If there is no remote port, do check for the direction of the
			// connection. This will be the case for pure checking functions
			// that do not want to change direction state.
			if pktInfo.RemotePort() == 0 {
				return CheckPID(socketInfo, pktInfo.Inbound)
			}

			// Get (and save) the direction of the connection.
			connInbound := table.getDirection(socketInfo, pktInfo)

			// Check we have the PID and return.
			return CheckPID(socketInfo, connInbound)
		}

		// Search less if we want to be fast.
		if fast && i >= fastLookupTries {
			break
		}
	}

	return socket.UndefinedProcessID, pktInfo.Inbound, ErrConnectionNotFound
}

func findUDPSocket(pktInfo *packet.Info, binds []*socket.BindInfo, isInboundMulticast bool) (socketInfo *socket.BindInfo) {
	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()

	// search binds
	for _, socketInfo := range binds {
		if localPort == socketInfo.Local.Port &&
			(socketInfo.ListensAny || // zero IP (dual-stack)
				isInboundMulticast || // inbound broadcast, multicast
				localIP.Equal(socketInfo.Local.IP)) {
			return socketInfo
		}
	}

	return nil
}
