package mgr

import (
	"sync"
	"time"
)

// SleepyTicker is wrapper over time.Ticker that respects the sleep mode of the module.
type SleepyTicker struct {
	ticker         *time.Ticker
	normalDuration time.Duration
	sleepDuration  time.Duration
	sleepMode      bool

	// lock guards sleepMode and sleepChannel.
	lock         sync.Mutex
	sleepChannel chan time.Time
}

// NewSleepyTicker returns a new SleepyTicker. This is a wrapper of the standard time.Ticker but it respects modules.Module sleep mode. Check https://pkg.go.dev/time#Ticker.
// If sleepDuration is set to 0 ticker will not tick during sleep.
func NewSleepyTicker(normalDuration time.Duration, sleepDuration time.Duration) *SleepyTicker {
	st := &SleepyTicker{
		ticker:         time.NewTicker(normalDuration),
		normalDuration: normalDuration,
		sleepDuration:  sleepDuration,
		sleepMode:      false,
	}

	return st
}

// Wait waits until the module is not in sleep mode and returns time.Ticker.C channel.
// If the ticker sleeps with sleepDuration == 0, the returned channel is closed when sleep mode ends.
func (st *SleepyTicker) Wait() <-chan time.Time {
	st.lock.Lock()
	defer st.lock.Unlock()

	if st.sleepMode && st.sleepDuration == 0 {
		return st.sleepChannel
	}
	return st.ticker.C
}

// Stop turns off a ticker. After Stop, no more ticks will be sent. Stop does not close the channel, to prevent a concurrent goroutine reading from the channel from seeing an erroneous "tick".
func (st *SleepyTicker) Stop() {
	st.ticker.Stop()
}

// SetSleep sets the sleep mode of the ticker. If enabled is true, the ticker will tick with sleepDuration. If enabled is false, the ticker will tick with normalDuration.
func (st *SleepyTicker) SetSleep(enabled bool) {
	st.lock.Lock()
	defer st.lock.Unlock()

	st.sleepMode = enabled
	if enabled {
		if st.sleepDuration > 0 {
			st.ticker.Reset(st.sleepDuration)
		} else if st.sleepChannel == nil {
			// Next call to Wait will wait until SetSleep is called with enabled == false.
			// An existing channel is kept so that current waiters are not leaked.
			st.sleepChannel = make(chan time.Time)
		}
	} else {
		st.ticker.Reset(st.normalDuration)
		if st.sleepChannel != nil {
			// Notify that we are not sleeping anymore.
			close(st.sleepChannel)
			st.sleepChannel = nil
		}
	}
}
