package mgr

import (
	"testing"
	"time"
)

func TestSleepyTickerStop(t *testing.T) {
	normalDuration := 100 * time.Millisecond
	sleepDuration := 200 * time.Millisecond

	st := NewSleepyTicker(normalDuration, sleepDuration)
	st.Stop() // no panic expected here
}

func TestSleepyTicker(t *testing.T) {
	normalDuration := 100 * time.Millisecond
	sleepDuration := 200 * time.Millisecond

	st := NewSleepyTicker(normalDuration, sleepDuration)

	// Test normal mode
	select {
	case <-st.Wait():
		// Expected tick
	case <-time.After(normalDuration + 50*time.Millisecond):
		t.Error("expected tick in normal mode")
	}

	// Test sleep mode
	st.SetSleep(true)
	select {
	case <-st.Wait():
		// Expected tick
	case <-time.After(sleepDuration + 50*time.Millisecond):
		t.Error("expected tick in sleep mode")
	}

	// Test sleep mode with sleepDuration == 0
	st = NewSleepyTicker(normalDuration, 0)
	st.SetSleep(true)
	select {
	case <-st.Wait():
		t.Error("did not expect tick when sleepDuration is 0")
	case <-time.After(normalDuration):
		// Expected no tick
	}

	// Test stopping the ticker
	st.Stop()
	select {
	case <-st.Wait():
		t.Error("did not expect tick after stopping the ticker")
	case <-time.After(normalDuration):
		// Expected no tick
	}
}
