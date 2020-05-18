package state

import (
	"context"
	"net"
	"time"

	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/socket"
)

type udpState struct {
	inbound  bool
	lastSeen time.Time
}

const (
	UpdConnStateTTL             = 72 * time.Hour
	UdpConnStateShortenedTTL    = 3 * time.Hour
	AggressiveCleaningThreshold = 256
)

var (
	udp4States = make(map[string]map[string]*udpState) // locked with udp4Lock
	udp6States = make(map[string]map[string]*udpState) // locked with udp6Lock
)

func getUDPConnState(socketInfo *socket.BindInfo, udpStates map[string]map[string]*udpState, remoteIP net.IP, remotePort uint16) (udpConnState *udpState, ok bool) {
	bindMap, ok := udpStates[makeUDPStateKey(socketInfo.Local.IP, socketInfo.Local.Port)]
	if ok {
		udpConnState, ok = bindMap[makeUDPStateKey(remoteIP, remotePort)]
		return
	}

	return nil, false
}

func getUDPDirection(socketInfo *socket.BindInfo, udpStates map[string]map[string]*udpState, pktInfo *packet.Info) (connDirection bool) {
	localKey := makeUDPStateKey(socketInfo.Local.IP, socketInfo.Local.Port)

	bindMap, ok := udpStates[localKey]
	if !ok {
		bindMap = make(map[string]*udpState)
		udpStates[localKey] = bindMap
	}

	remoteKey := makeUDPStateKey(pktInfo.RemoteIP(), pktInfo.RemotePort())
	udpConnState, ok := bindMap[remoteKey]
	if !ok {
		bindMap[remoteKey] = &udpState{
			inbound:  pktInfo.Inbound,
			lastSeen: time.Now().UTC(),
		}
		return pktInfo.Inbound
	}

	udpConnState.lastSeen = time.Now().UTC()
	return udpConnState.inbound
}

func CleanUDPStates(ctx context.Context) {
	now := time.Now().UTC()

	udp4Lock.Lock()
	updateUDP4Table()
	cleanStates(ctx, udp4Binds, udp4States, now)
	udp4Lock.Unlock()

	udp6Lock.Lock()
	updateUDP6Table()
	cleanStates(ctx, udp6Binds, udp6States, now)
	udp6Lock.Unlock()
}

func cleanStates(
	ctx context.Context,
	binds []*socket.BindInfo,
	udpStates map[string]map[string]*udpState,
	now time.Time,
) {
	// compute thresholds
	threshold := now.Add(-UpdConnStateTTL)
	shortThreshhold := now.Add(-UdpConnStateShortenedTTL)

	// make list of all active keys
	bindKeys := make(map[string]struct{})
	for _, socketInfo := range binds {
		bindKeys[makeUDPStateKey(socketInfo.Local.IP, socketInfo.Local.Port)] = struct{}{}
	}

	// clean the udp state storage
	for localKey, bindMap := range udpStates {
		_, active := bindKeys[localKey]
		if active {
			// clean old entries
			for remoteKey, udpConnState := range bindMap {
				if udpConnState.lastSeen.Before(threshold) {
					delete(bindMap, remoteKey)
				}
			}
			// if there are too many clean more aggressively
			if len(bindMap) > AggressiveCleaningThreshold {
				for remoteKey, udpConnState := range bindMap {
					if udpConnState.lastSeen.Before(shortThreshhold) {
						delete(bindMap, remoteKey)
					}
				}
			}
		} else {
			// delete the whole thing
			delete(udpStates, localKey)
		}
	}
}

func makeUDPStateKey(ip net.IP, port uint16) string {
	// This could potentially go wrong, but as all IPs are created by the same source, everything should be fine.
	return string(ip) + string(port)
}
