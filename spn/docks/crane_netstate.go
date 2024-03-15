package docks

import (
	"sync"
	"sync/atomic"
	"time"
)

// NetStatePeriodInterval defines the interval some of the net state should be reset.
const NetStatePeriodInterval = 15 * time.Minute

// NetworkOptimizationState holds data for optimization purposes.
type NetworkOptimizationState struct {
	lock sync.Mutex

	// lastSuggestedAt holds the time when the connection to the connected Hub was last suggested by the network optimization.
	lastSuggestedAt time.Time

	// stoppingRequested signifies whether stopping this lane is requested.
	stoppingRequested bool
	// stoppingRequestedByPeer signifies whether stopping this lane is requested by the peer.
	stoppingRequestedByPeer bool
	// markedStoppingAt holds the time when the crane was last marked as stopping.
	markedStoppingAt time.Time

	lifetimeBytesIn  *uint64
	lifetimeBytesOut *uint64
	lifetimeStarted  time.Time
	periodBytesIn    *uint64
	periodBytesOut   *uint64
	periodStarted    time.Time
}

func newNetworkOptimizationState() *NetworkOptimizationState {
	return &NetworkOptimizationState{
		lifetimeBytesIn:  new(uint64),
		lifetimeBytesOut: new(uint64),
		lifetimeStarted:  time.Now(),
		periodBytesIn:    new(uint64),
		periodBytesOut:   new(uint64),
		periodStarted:    time.Now(),
	}
}

// UpdateLastSuggestedAt sets when the lane was last suggested to the current time.
func (netState *NetworkOptimizationState) UpdateLastSuggestedAt() {
	netState.lock.Lock()
	defer netState.lock.Unlock()

	netState.lastSuggestedAt = time.Now()
}

// StoppingState returns when the stopping state.
func (netState *NetworkOptimizationState) StoppingState() (requested, requestedByPeer bool, markedAt time.Time) {
	netState.lock.Lock()
	defer netState.lock.Unlock()

	return netState.stoppingRequested, netState.stoppingRequestedByPeer, netState.markedStoppingAt
}

// RequestStoppingSuggested returns whether the crane should request stopping.
func (netState *NetworkOptimizationState) RequestStoppingSuggested(maxNotSuggestedDuration time.Duration) bool {
	netState.lock.Lock()
	defer netState.lock.Unlock()

	return time.Now().Add(-maxNotSuggestedDuration).After(netState.lastSuggestedAt)
}

// StoppingSuggested returns whether the crane should be marked as stopping.
func (netState *NetworkOptimizationState) StoppingSuggested() bool {
	netState.lock.Lock()
	defer netState.lock.Unlock()

	return netState.stoppingRequested &&
		netState.stoppingRequestedByPeer
}

// StopSuggested returns whether the crane should be stopped.
func (netState *NetworkOptimizationState) StopSuggested() bool {
	netState.lock.Lock()
	defer netState.lock.Unlock()

	return netState.stoppingRequested &&
		netState.stoppingRequestedByPeer &&
		!netState.markedStoppingAt.IsZero() &&
		time.Now().Add(-maxCraneStoppingDuration).After(netState.markedStoppingAt)
}

// ReportTraffic adds the reported transferred data to the traffic stats.
func (netState *NetworkOptimizationState) ReportTraffic(bytes uint64, in bool) {
	if in {
		atomic.AddUint64(netState.lifetimeBytesIn, bytes)
		atomic.AddUint64(netState.periodBytesIn, bytes)
	} else {
		atomic.AddUint64(netState.lifetimeBytesOut, bytes)
		atomic.AddUint64(netState.periodBytesOut, bytes)
	}
}

// LapsePeriod lapses the net state period, if needed.
func (netState *NetworkOptimizationState) LapsePeriod() {
	netState.lock.Lock()
	defer netState.lock.Unlock()

	// Reset period if interval elapsed.
	if time.Now().Add(-NetStatePeriodInterval).After(netState.periodStarted) {
		atomic.StoreUint64(netState.periodBytesIn, 0)
		atomic.StoreUint64(netState.periodBytesOut, 0)
		netState.periodStarted = time.Now()
	}
}

// GetTrafficStats returns the traffic stats.
func (netState *NetworkOptimizationState) GetTrafficStats() (
	lifetimeBytesIn uint64,
	lifetimeBytesOut uint64,
	lifetimeStarted time.Time,
	periodBytesIn uint64,
	periodBytesOut uint64,
	periodStarted time.Time,
) {
	netState.lock.Lock()
	defer netState.lock.Unlock()

	return atomic.LoadUint64(netState.lifetimeBytesIn),
		atomic.LoadUint64(netState.lifetimeBytesOut),
		netState.lifetimeStarted,
		atomic.LoadUint64(netState.periodBytesIn),
		atomic.LoadUint64(netState.periodBytesOut),
		netState.periodStarted
}
