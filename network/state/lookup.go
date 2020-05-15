package state

import (
	"errors"
	"net"
	"sync"
	"time"

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

const (
	UnidentifiedProcessID = -1
)

// Errors
var (
	ErrConnectionNotFound = errors.New("could not find connection in system state tables")
	ErrPIDNotFound        = errors.New("could not find pid for socket inode")
)

var (
	tcp4Lock sync.Mutex
	tcp6Lock sync.Mutex
	udp4Lock sync.Mutex
	udp6Lock sync.Mutex

	waitTime = 3 * time.Millisecond
)

func LookupWithPacket(pkt packet.Packet) (pid int, inbound bool, err error) {
	meta := pkt.Info()
	return Lookup(
		meta.Version,
		meta.Protocol,
		meta.LocalIP(),
		meta.LocalPort(),
		meta.RemoteIP(),
		meta.RemotePort(),
		meta.Direction,
	)
}

func Lookup(
	ipVersion packet.IPVersion,
	protocol packet.IPProtocol,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
	pktInbound bool,
) (
	pid int,
	inbound bool,
	err error,
) {

	// auto-detect version
	if ipVersion == 0 {
		if ip := localIP.To4(); ip != nil {
			ipVersion = packet.IPv4
		} else {
			ipVersion = packet.IPv6
		}
	}

	switch {
	case ipVersion == packet.IPv4 && protocol == packet.TCP:
		tcp4Lock.Lock()
		defer tcp4Lock.Unlock()
		return searchTCP(tcp4Connections, tcp4Listeners, updateTCP4Tables, localIP, localPort)

	case ipVersion == packet.IPv6 && protocol == packet.TCP:
		tcp6Lock.Lock()
		defer tcp6Lock.Unlock()
		return searchTCP(tcp6Connections, tcp6Listeners, updateTCP6Tables, localIP, localPort)

	case ipVersion == packet.IPv4 && protocol == packet.UDP:
		udp4Lock.Lock()
		defer udp4Lock.Unlock()
		return searchUDP(udp4Binds, udp4States, updateUDP4Table, localIP, localPort, remoteIP, remotePort, pktInbound)

	case ipVersion == packet.IPv6 && protocol == packet.UDP:
		udp6Lock.Lock()
		defer udp6Lock.Unlock()
		return searchUDP(udp6Binds, udp6States, updateUDP6Table, localIP, localPort, remoteIP, remotePort, pktInbound)

	default:
		return UnidentifiedProcessID, false, errors.New("unsupported protocol for finding process")
	}
}

func searchTCP(
	connections []*socket.ConnectionInfo,
	listeners []*socket.BindInfo,
	updateTables func() ([]*socket.ConnectionInfo, []*socket.BindInfo),
	localIP net.IP,
	localPort uint16,
) (
	pid int,
	inbound bool,
	err error,
) {

	// search until we find something
	for i := 0; i < 5; i++ {
		// always search listeners first
		for _, socketInfo := range listeners {
			if localPort == socketInfo.Local.Port &&
				(socketInfo.Local.IP[0] == 0 || localIP.Equal(socketInfo.Local.IP)) {
				return checkBindPID(socketInfo, true)
			}
		}

		// search connections
		for _, socketInfo := range connections {
			if localPort == socketInfo.Local.Port &&
				localIP.Equal(socketInfo.Local.IP) {
				return checkConnectionPID(socketInfo, false)
			}
		}

		// we found nothing, we could have been too fast, give the kernel some time to think
		time.Sleep(waitTime)

		// refetch lists
		connections, listeners = updateTables()
	}

	return UnidentifiedProcessID, false, ErrConnectionNotFound
}

func searchUDP(
	binds []*socket.BindInfo,
	udpStates map[string]map[string]*udpState,
	updateTable func() []*socket.BindInfo,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
	pktInbound bool,
) (
	pid int,
	inbound bool,
	err error,
) {

	// search until we find something
	for i := 0; i < 5; i++ {
		// search binds
		for _, socketInfo := range binds {
			if localPort == socketInfo.Local.Port &&
				(socketInfo.Local.IP[0] == 0 || localIP.Equal(socketInfo.Local.IP)) {

				// do not check direction if remoteIP/Port is not given
				if remotePort == 0 {
					return checkBindPID(socketInfo, pktInbound)
				}

				// get direction and return
				connInbound := getUDPDirection(socketInfo, udpStates, remoteIP, remotePort, pktInbound)
				return checkBindPID(socketInfo, connInbound)
			}
		}

		// we found nothing, we could have been too fast, give the kernel some time to think
		time.Sleep(waitTime)

		// refetch lists
		binds = updateTable()
	}

	return UnidentifiedProcessID, pktInbound, ErrConnectionNotFound
}
