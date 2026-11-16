package state

import (
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/network/socket"
)

type udpTable struct {
	version int

	binds []*socket.BindInfo
	lock  sync.RWMutex

	// lastUpdateAt stores the time when the tables where last updated as unix nanoseconds.
	lastUpdateAt atomic.Int64

	fetchLimiter *utils.CallLimiter2
	fetchTable   func() (binds []*socket.BindInfo, err error)

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
		version:      6,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable:   getUDP6Table,
		states:       make(map[string]map[string]*udpState),
	}

	udp4Table = &udpTable{
		version:      4,
		fetchLimiter: utils.NewCallLimiter2(minDurationBetweenTableUpdates),
		fetchTable:   getUDP4Table,
		states:       make(map[string]map[string]*udpState),
	}
)

// EnableUDPDualStack adds the UDP6 table to the UDP4 table as a dual-stack.
// Must be called before any lookup operation.
func EnableUDPDualStack() {
	udp4Table.dualStack = udp6Table
}

func (table *udpTable) getCurrentTables() (binds []*socket.BindInfo) {
	table.lock.RLock()
	defer table.lock.RUnlock()

	return table.binds
}

func (table *udpTable) updateTables() (binds []*socket.BindInfo) {
	// Fetch tables.
	table.fetchLimiter.Do(func() {
		// Fetch new tables from system.
		binds, err := table.fetchTable()
		if err != nil {
			log.Warningf("state: failed to get UDP%d socket table: %s", table.version, err)
			return
		}

		// Pre-check for any listeners.
		for _, bindInfo := range binds {
			bindInfo.ListensAny = bindInfo.Local.IP.Equal(net.IPv4zero) || bindInfo.Local.IP.Equal(net.IPv6zero)
		}

		// Apply new tables.
		table.lock.Lock()
		defer table.lock.Unlock()
		table.binds = binds
		table.lastUpdateAt.Store(time.Now().UnixNano())
	})

	return table.getCurrentTables()
}

// CleanUDPStates cleans the udp connection states which save connection directions.
func CleanUDPStates(_ context.Context) {
	now := time.Now().UTC()

	// Update the dual-stack table before cleaning the UDP4 table, as the UDP4
	// table also holds the states of dual-stack sockets.
	if udp4Table.dualStack != nil {
		udp4Table.dualStack.updateTables()
	}
	udp4Table.updateTables()
	udp4Table.cleanStates(now)

	if netenv.IPv6Enabled() {
		udp6Table.updateTables()
		udp6Table.cleanStates(now)
	}
}

// getConnState returns a copy of the connection state, so that it can be
// used without holding the states lock.
func (table *udpTable) getConnState(
	socketInfo *socket.BindInfo,
	remoteAddress socket.Address,
) (udpConnState udpState, ok bool) {
	// Build the keys in stack buffers, so that the lookup does not allocate.
	var localKeyBuf, remoteKeyBuf [udpStateKeyBufSize]byte
	localKey := appendUDPStateKey(localKeyBuf[:0], socketInfo.Local)
	remoteKey := appendUDPStateKey(remoteKeyBuf[:0], remoteAddress)

	table.statesLock.Lock()
	defer table.statesLock.Unlock()

	bindMap, ok := table.states[string(localKey)]
	if !ok {
		return udpState{}, false
	}
	state, ok := bindMap[string(remoteKey)]
	if !ok {
		return udpState{}, false
	}
	return *state, true
}

func (table *udpTable) getDirection(
	socketInfo *socket.BindInfo,
	pktInfo *packet.Info,
) (connDirection bool) {
	table.statesLock.Lock()
	defer table.statesLock.Unlock()

	// Build the keys in stack buffers, so that looking up existing state does
	// not allocate. Strings are only allocated when new state is stored.
	var localKeyBuf, remoteKeyBuf [udpStateKeyBufSize]byte
	localKey := appendUDPStateKey(localKeyBuf[:0], socketInfo.Local)

	bindMap, ok := table.states[string(localKey)]
	if !ok {
		bindMap = make(map[string]*udpState)
		table.states[string(localKey)] = bindMap
	}

	remoteKey := appendUDPStateKey(remoteKeyBuf[:0], socket.Address{
		IP:   pktInfo.RemoteIP(),
		Port: pktInfo.RemotePort(),
	})
	udpConnState, ok := bindMap[string(remoteKey)]
	if !ok {
		bindMap[string(remoteKey)] = &udpState{
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
	// Lookups store the states of dual-stack sockets in this table, so the
	// sockets of the dual-stack table are active too.
	if table.dualStack != nil {
		for _, socketInfo := range table.dualStack.getCurrentTables() {
			bindKeys[makeUDPStateKey(socketInfo.Local)] = struct{}{}
		}
	}

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

// udpStateKeyBufSize fits a 16 byte IP and a 5 digit port.
const udpStateKeyBufSize = 21

func makeUDPStateKey(address socket.Address) string {
	var buf [udpStateKeyBufSize]byte
	return string(appendUDPStateKey(buf[:0], address))
}

// appendUDPStateKey appends the state key of the given address to buf.
func appendUDPStateKey(buf []byte, address socket.Address) []byte {
	// This could potentially go wrong, but as all IPs are created by the same source, everything should be fine.
	buf = append(buf, address.IP...)
	return strconv.AppendUint(buf, uint64(address.Port), 10)
}
