package state

import (
	"context"
	"sync"
	"time"

	"github.com/safing/portbase/utils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/socket"
)

type udpTable struct {
	version int

	binds []*socket.BindInfo
	lock  sync.RWMutex

	fetchOnceAgain utils.OnceAgain
	fetchTable     func() (binds []*socket.BindInfo, err error)

	states     map[string]map[string]*udpState
	statesLock sync.Mutex
}

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
	udp4Table = &udpTable{
		version:    4,
		fetchTable: getUDP4Table,
		states:     make(map[string]map[string]*udpState),
	}

	udp6Table = &udpTable{
		version:    6,
		fetchTable: getUDP6Table,
		states:     make(map[string]map[string]*udpState),
	}
)

// CleanUDPStates cleans the udp connection states which save connection directions.
func CleanUDPStates(_ context.Context) {
	now := time.Now().UTC()

	udp4Table.updateTable()
	udp4Table.cleanStates(now)

	udp6Table.updateTable()
	udp6Table.cleanStates(now)
}

func (table *udpTable) getConnState(
	socketInfo *socket.BindInfo,
	remoteAddress socket.Address,
) (udpConnState *udpState, ok bool) {
	table.statesLock.Lock()
	defer table.statesLock.Unlock()

	bindMap, ok := table.states[makeUDPStateKey(socketInfo.Local)]
	if ok {
		udpConnState, ok = bindMap[makeUDPStateKey(remoteAddress)]
		return
	}

	return nil, false
}

func (table *udpTable) getDirection(
	socketInfo *socket.BindInfo,
	pktInfo *packet.Info,
) (connDirection bool) {
	table.statesLock.Lock()
	defer table.statesLock.Unlock()

	localKey := makeUDPStateKey(socketInfo.Local)

	bindMap, ok := table.states[localKey]
	if !ok {
		bindMap = make(map[string]*udpState)
		table.states[localKey] = bindMap
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

func (table *udpTable) cleanStates(now time.Time) {

	// compute thresholds
	threshold := now.Add(-UDPConnStateTTL)
	shortThreshhold := now.Add(-UDPConnStateShortenedTTL)

	// make lookup map of all active keys
	bindKeys := make(map[string]struct{})
	table.lock.RLock()
	for _, socketInfo := range table.binds {
		bindKeys[makeUDPStateKey(socketInfo.Local)] = struct{}{}
	}
	table.lock.RUnlock()

	table.statesLock.Lock()
	defer table.statesLock.Unlock()

	// clean the udp state storage
	for localKey, bindMap := range table.states {
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
			delete(table.states, localKey)
		}
	}
}

func makeUDPStateKey(address socket.Address) string {
	// This could potentially go wrong, but as all IPs are created by the same source, everything should be fine.
	return string(address.IP) + string(address.Port)
}
