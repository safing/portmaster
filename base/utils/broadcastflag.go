package utils

import (
	"sync"

	"github.com/tevino/abool"
)

// BroadcastFlag is a simple system to broadcast a flag value.
type BroadcastFlag struct {
	flag   *abool.AtomicBool
	signal chan struct{}
	lock   sync.Mutex
}

// Flag receives changes from its broadcasting flag.
// A Flag must only be used in one goroutine and is not concurrency safe,
// but fast.
type Flag struct {
	flag        *abool.AtomicBool
	signal      chan struct{}
	broadcaster *BroadcastFlag
}

// NewBroadcastFlag returns a new BroadcastFlag.
// In the initial state, the flag is not set and the signal does not trigger.
func NewBroadcastFlag() *BroadcastFlag {
	return &BroadcastFlag{
		flag:   abool.New(),
		signal: make(chan struct{}),
		lock:   sync.Mutex{},
	}
}

// NewFlag returns a new Flag that listens to this broadcasting flag.
// In the initial state, the flag is set and the signal triggers.
// You can call Refresh immediately to get the current state from the
// broadcasting flag.
func (bf *BroadcastFlag) NewFlag() *Flag {
	newFlag := &Flag{
		flag:        abool.NewBool(true),
		signal:      make(chan struct{}),
		broadcaster: bf,
	}
	close(newFlag.signal)
	return newFlag
}

// NotifyAndReset notifies all flags of this broadcasting flag and resets the
// internal broadcast flag state.
func (bf *BroadcastFlag) NotifyAndReset() {
	bf.lock.Lock()
	defer bf.lock.Unlock()

	// Notify all flags of the change.
	bf.flag.Set()
	close(bf.signal)

	// Reset
	bf.flag = abool.New()
	bf.signal = make(chan struct{})
}

// Signal returns a channel that waits for the flag to be set. This does not
// reset the Flag itself, you'll need to call Refresh for that.
func (f *Flag) Signal() <-chan struct{} {
	return f.signal
}

// IsSet returns whether the flag was set since the last Refresh.
// This does not reset the Flag itself, you'll need to call Refresh for that.
func (f *Flag) IsSet() bool {
	return f.flag.IsSet()
}

// Refresh fetches the current state from the broadcasting flag.
func (f *Flag) Refresh() {
	f.broadcaster.lock.Lock()
	defer f.broadcaster.lock.Unlock()

	// Copy current flag and signal from the broadcasting flag.
	f.flag = f.broadcaster.flag
	f.signal = f.broadcaster.signal
}
