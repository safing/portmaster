package state

import (
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/network/socket"
)

type udpTable struct {
	version int

	binds []*socket.BindInfo
	lock  sync.RWMutex

	updateIter atomic.Uint64
	// lastUpdateAt stores the time when the tables where last updated as unix nanoseconds.
	lastUpdateAt atomic.Int64

	fetchingLock       sync.Mutex
	fetchingInProgress bool
	fetchingDoneSignal chan struct{}
	fetchTable         func() (binds []*socket.BindInfo, err error)

	states     map[string]map[string]*udpState
	statesLock sync.Mutex

	dualStack *udpTable
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
	udp6Table = &udpTable{
		version:            6,
		fetchingDoneSignal: make(chan struct{}),
		fetchTable:         getUDP6Table,
		states:             make(map[string]map[string]*udpState),
	}

	udp4Table = &udpTable{
		version:            4,
		fetchingDoneSignal: make(chan struct{}),
		fetchTable:         getUDP4Table,
		states:             make(map[string]map[string]*udpState),
	}
)

// EnableUDPDualStack adds the UDP6 table to the UDP4 table as a dual-stack.
// Must be called before any lookup operation.
func EnableUDPDualStack() {
	udp4Table.dualStack = udp6Table
}

func (table *udpTable) getCurrentTables() (
	binds []*socket.BindInfo,
	updateIter uint64,
) {
	table.lock.RLock()
	defer table.lock.RUnlock()

	return table.binds, table.updateIter.Load()
}

func (table *udpTable) checkFetchingState() (fetch bool, signal chan struct{}) {
	table.fetchingLock.Lock()
	defer table.fetchingLock.Unlock()

	// If fetching is already in progress, just return the signal.
	if table.fetchingInProgress {
		return false, table.fetchingDoneSignal
	}

	// Otherwise, tell caller to fetch.
	table.fetchingInProgress = true
	return true, nil
}

func (table *udpTable) signalFetchComplete() {
	table.fetchingLock.Lock()
	defer table.fetchingLock.Unlock()

	// Set fetching state.
	table.fetchingInProgress = false

	// Signal waiting goroutines.
	close(table.fetchingDoneSignal)
	table.fetchingDoneSignal = make(chan struct{})
}

func (table *udpTable) updateTables(previousUpdateIter uint64) (
	binds []*socket.BindInfo,
	updateIter uint64,
) {
	var tries int

	// Attempt to update the tables until we get a new version of the tables.
	for previousUpdateIter == table.updateIter.Load() {
		// Abort if it takes too long.
		tries++
		if tries > maxUpdateTries {
			log.Warningf("state: failed to upate UDP%d socket table %d times", table.version, tries-1)
			return table.getCurrentTables()
		}

		// Check if someone is fetching or if we should fetch.
		fetch, signal := table.checkFetchingState()
		if fetch {
			defer table.signalFetchComplete()

			// Just to be sure, check again if there is a new version.
			if previousUpdateIter < table.updateIter.Load() {
				return table.getCurrentTables()
			}

			// Wait for 5 milliseconds.
			time.Sleep(5 * time.Millisecond)

			// Fetch new tables from system.
			binds, err := table.fetchTable()
			if err != nil {
				log.Warningf("state: failed to get UDP%d socket table: %s", table.version, err)
				// Return the current tables as fallback, as we need to trigger the defer to complete the fetch.
				return table.getCurrentTables()
			}

			// Pre-check for any listeners.
			for _, bindInfo := range binds {
				bindInfo.ListensAny = bindInfo.Local.IP.Equal(net.IPv4zero) || bindInfo.Local.IP.Equal(net.IPv6zero)
			}

			// Apply new tables.
			table.lock.Lock()
			defer table.lock.Unlock()
			table.binds = binds
			table.updateIter.Add(1)
			table.lastUpdateAt.Store(time.Now().UnixNano())

			// Return new tables immediately.
			return table.binds, table.updateIter.Load()
		}

		// Otherwise, wait for fetch to complete.
		<-signal
	}

	return table.getCurrentTables()
}

// CleanUDPStates cleans the udp connection states which save connection directions.
func CleanUDPStates(_ context.Context) {
	now := time.Now().UTC()

	udp4Table.updateTables(udp4Table.updateIter.Load())
	udp4Table.cleanStates(now)

	if netenv.IPv6Enabled() {
		udp6Table.updateTables(udp6Table.updateIter.Load())
		udp6Table.cleanStates(now)
	}
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
	return string(address.IP) + strconv.Itoa(int(address.Port))
}
