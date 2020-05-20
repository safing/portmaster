package state

import (
	"context"
	"time"

	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/socket"
)

type udpState struct {
	inbound  bool
	lastSeen time.Time
}

const (
	// UDPConnStateTTL is the maximum time a udp connection state is held.
	UDPConnStateTTL = 72 * time.Hour

	// UDPConnStateShortenedTTL is a shortened maximum time a udp connection state is held, if there more entries than defined by AggressiveCleaningThreshold.
	UDPConnStateShortenedTTL = 3 * time.Hour

	// AggressiveCleaningThreshold defines the soft limit of udp connection state held per udp socket.
	AggressiveCleaningThreshold = 256
)

var (
	udp4States = make(map[string]map[string]*udpState) // locked with udp4Lock
	udp6States = make(map[string]map[string]*udpState) // locked with udp6Lock
)

func getUDPConnState(socketInfo *socket.BindInfo, udpStates map[string]map[string]*udpState, remoteAddress socket.Address) (udpConnState *udpState, ok bool) {
	bindMap, ok := udpStates[makeUDPStateKey(socketInfo.Local)]
	if ok {
		udpConnState, ok = bindMap[makeUDPStateKey(remoteAddress)]
		return
	}

	return nil, false
}

func getUDPDirection(socketInfo *socket.BindInfo, udpStates map[string]map[string]*udpState, pktInfo *packet.Info) (connDirection bool) {
	localKey := makeUDPStateKey(socketInfo.Local)

	bindMap, ok := udpStates[localKey]
	if !ok {
		bindMap = make(map[string]*udpState)
		udpStates[localKey] = bindMap
	}

	remoteKey := makeUDPStateKey(socket.Address{
		IP:   pktInfo.RemoteIP(),
		Port: pktInfo.RemotePort(),
	})
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

// CleanUDPStates cleans the udp connection states which save connection directions.
func CleanUDPStates(_ context.Context) {
	now := time.Now().UTC()

	udp4Lock.Lock()
	updateUDP4Table()
	cleanStates(udp4Binds, udp4States, now)
	udp4Lock.Unlock()

	udp6Lock.Lock()
	updateUDP6Table()
	cleanStates(udp6Binds, udp6States, now)
	udp6Lock.Unlock()
}

func cleanStates(
	binds []*socket.BindInfo,
	udpStates map[string]map[string]*udpState,
	now time.Time,
) {
	// compute thresholds
	threshold := now.Add(-UDPConnStateTTL)
	shortThreshhold := now.Add(-UDPConnStateShortenedTTL)

	// make lookup map of all active keys
	bindKeys := make(map[string]struct{})
	for _, socketInfo := range binds {
		bindKeys[makeUDPStateKey(socketInfo.Local)] = struct{}{}
	}

	// clean the udp state storage
	for localKey, bindMap := range udpStates {
		if _, active := bindKeys[localKey]; active {
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

func makeUDPStateKey(address socket.Address) string {
	// This could potentially go wrong, but as all IPs are created by the same source, everything should be fine.
	return string(address.IP) + string(address.Port)
}
