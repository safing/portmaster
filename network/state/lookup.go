package state

import (
	"errors"
	"time"

	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/socket"
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

var (
	baseWaitTime      = 3 * time.Millisecond
	lookupRetries     = 7 * 2 // Every retry takes two full passes.
	fastLookupRetries = 2 * 2
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
	// Search pattern: search, refresh, search, wait, search, refresh, search, wait, ...

	// Search for the socket until found.
	for i := 1; i <= lookupRetries; i++ {
		// Check main table for socket.
		socketInfo, inbound := table.findSocket(pktInfo)
		if socketInfo == nil && table.dualStack != nil {
			// If there was no match in the main table and we are dual-stack, check
			// the dual-stack table for the socket.
			socketInfo, inbound = table.dualStack.findSocket(pktInfo)
		}

		// If there's a match, check we have the PID and return.
		if socketInfo != nil {
			return checkPID(socketInfo, inbound)
		}

		// Search less if we want to be fast.
		if fast && i < fastLookupRetries {
			break
		}

		// every time, except for the last iteration
		if i < lookupRetries {
			// Take turns in waiting and refreshing in order to satisfy the search pattern.
			if i%2 == 0 {
				// we found nothing, we could have been too fast, give the kernel some time to think
				// back off timer: with 3ms baseWaitTime: 3, 6, 9, 12, 15, 18, 21ms - 84ms in total
				time.Sleep(time.Duration(i+1) * baseWaitTime)
			} else {
				// refetch lists
				table.updateTables()
				if table.dualStack != nil {
					table.dualStack.updateTables()
				}
			}
		}
	}

	return socket.UndefinedProcessID, pktInfo.Inbound, ErrConnectionNotFound
}

func (table *tcpTable) findSocket(pktInfo *packet.Info) (
	socketInfo socket.Info,
	inbound bool,
) {
	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()

	table.lock.RLock()
	defer table.lock.RUnlock()

	// always search listeners first
	for _, socketInfo := range table.listeners {
		if localPort == socketInfo.Local.Port &&
			(socketInfo.ListensAny || localIP.Equal(socketInfo.Local.IP)) {
			return socketInfo, true
		}
	}

	// search connections
	for _, socketInfo := range table.connections {
		if localPort == socketInfo.Local.Port &&
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
	// Search pattern: search, refresh, search, wait, search, refresh, search, wait, ...

	// TODO: Currently broadcast/multicast scopes are not checked, so we might
	// attribute an incoming broadcast/multicast packet to the wrong process if
	// there are multiple processes listening on the same local port, but
	// binding to different addresses. This highly unusual for clients.
	isInboundMulticast := pktInfo.Inbound && netutils.GetIPScope(pktInfo.LocalIP()) == netutils.LocalMulticast

	// Search for the socket until found.
	for i := 1; i <= lookupRetries; i++ {
		// Check main table for socket.
		socketInfo := table.findSocket(pktInfo, isInboundMulticast)
		if socketInfo == nil && table.dualStack != nil {
			// If there was no match in the main table and we are dual-stack, check
			// the dual-stack table for the socket.
			socketInfo = table.dualStack.findSocket(pktInfo, isInboundMulticast)
		}

		// If there's a match, get the direction and check we have the PID, then return.
		if socketInfo != nil {
			// If there is no remote port, do check for the direction of the
			// connection. This will be the case for pure checking functions
			// that do not want to change direction state.
			if pktInfo.RemotePort() == 0 {
				return checkPID(socketInfo, pktInfo.Inbound)
			}

			// Get (and save) the direction of the connection.
			connInbound := table.getDirection(socketInfo, pktInfo)

			// Check we have the PID and return.
			return checkPID(socketInfo, connInbound)
		}

		// Search less if we want to be fast.
		if fast && i < fastLookupRetries {
			break
		}

		// every time, except for the last iteration
		if i < lookupRetries {
			// Take turns in waiting and refreshing in order to satisfy the search pattern.
			if i%2 == 0 {
				// we found nothing, we could have been too fast, give the kernel some time to think
				// back off timer: with 3ms baseWaitTime: 3, 6, 9, 12, 15, 18, 21ms - 84ms in total
				time.Sleep(time.Duration(i+1) * baseWaitTime)
			} else {
				// refetch lists
				table.updateTable()
				if table.dualStack != nil {
					table.dualStack.updateTable()
				}
			}
		}
	}

	return socket.UndefinedProcessID, pktInfo.Inbound, ErrConnectionNotFound
}

func (table *udpTable) findSocket(pktInfo *packet.Info, isInboundMulticast bool) (socketInfo *socket.BindInfo) {
	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()

	table.lock.RLock()
	defer table.lock.RUnlock()

	// search binds
	for _, socketInfo := range table.binds {
		if localPort == socketInfo.Local.Port &&
			(socketInfo.ListensAny || // zero IP (dual-stack)
				isInboundMulticast || // inbound broadcast, multicast
				localIP.Equal(socketInfo.Local.IP)) {
			return socketInfo
		}
	}

	return nil
}
