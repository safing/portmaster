package state

import (
	"net"
	"time"

	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/socket"
)

const (
	UDPConnectionTTL = 10 * time.Minute
)

func Exists(
	ipVersion packet.IPVersion,
	protocol packet.IPProtocol,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
	now time.Time,
) (exists bool) {

	switch {
	case ipVersion == packet.IPv4 && protocol == packet.TCP:
		tcp4Lock.Lock()
		defer tcp4Lock.Unlock()
		return existsTCP(tcp4Connections, localIP, localPort, remoteIP, remotePort)

	case ipVersion == packet.IPv6 && protocol == packet.TCP:
		tcp6Lock.Lock()
		defer tcp6Lock.Unlock()
		return existsTCP(tcp6Connections, localIP, localPort, remoteIP, remotePort)

	case ipVersion == packet.IPv4 && protocol == packet.UDP:
		udp4Lock.Lock()
		defer udp4Lock.Unlock()
		return existsUDP(udp4Binds, udp4States, localIP, localPort, remoteIP, remotePort, now)

	case ipVersion == packet.IPv6 && protocol == packet.UDP:
		udp6Lock.Lock()
		defer udp6Lock.Unlock()
		return existsUDP(udp6Binds, udp6States, localIP, localPort, remoteIP, remotePort, now)

	default:
		return false
	}
}

func existsTCP(
	connections []*socket.ConnectionInfo,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
) (exists bool) {

	// search connections
	for _, socketInfo := range connections {
		if localPort == socketInfo.Local.Port &&
			remotePort == socketInfo.Remote.Port &&
			remoteIP.Equal(socketInfo.Remote.IP) &&
			localIP.Equal(socketInfo.Local.IP) {
			return true
		}
	}

	return false
}

func existsUDP(
	binds []*socket.BindInfo,
	udpStates map[string]map[string]*udpState,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
	now time.Time,
) (exists bool) {

	connThreshhold := now.Add(-UDPConnectionTTL)

	// search binds
	for _, socketInfo := range binds {
		if localPort == socketInfo.Local.Port &&
			(socketInfo.Local.IP[0] == 0 || localIP.Equal(socketInfo.Local.IP)) {

			udpConnState, ok := getUDPConnState(socketInfo, udpStates, remoteIP, remotePort)
			switch {
			case !ok:
				return false
			case udpConnState.lastSeen.After(connThreshhold):
				return true
			default:
				return false
			}

		}
	}

	return false
}
