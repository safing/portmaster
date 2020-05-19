package state

import (
	"time"

	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/socket"
)

const (
	UDPConnectionTTL = 10 * time.Minute
)

func Exists(pktInfo *packet.Info, now time.Time) (exists bool) {
	switch {
	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.TCP:
		tcp4Lock.Lock()
		defer tcp4Lock.Unlock()
		return existsTCP(tcp4Connections, pktInfo)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.TCP:
		tcp6Lock.Lock()
		defer tcp6Lock.Unlock()
		return existsTCP(tcp6Connections, pktInfo)

	case pktInfo.Version == packet.IPv4 && pktInfo.Protocol == packet.UDP:
		udp4Lock.Lock()
		defer udp4Lock.Unlock()
		return existsUDP(udp4Binds, udp4States, pktInfo, now)

	case pktInfo.Version == packet.IPv6 && pktInfo.Protocol == packet.UDP:
		udp6Lock.Lock()
		defer udp6Lock.Unlock()
		return existsUDP(udp6Binds, udp6States, pktInfo, now)

	default:
		return false
	}
}

func existsTCP(connections []*socket.ConnectionInfo, pktInfo *packet.Info) (exists bool) {
	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()
	remoteIP := pktInfo.RemoteIP()
	remotePort := pktInfo.RemotePort()

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
	pktInfo *packet.Info,
	now time.Time,
) (exists bool) {

	localIP := pktInfo.LocalIP()
	localPort := pktInfo.LocalPort()
	remoteIP := pktInfo.RemoteIP()
	remotePort := pktInfo.RemotePort()

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
