package mgr

import (
	"testing"
	"time"
)

func TestSleepyTickerWakeupWithoutSleepDuration(t *testing.T) {
	t.Parallel()

	st := NewSleepyTicker(time.Hour, 0)
	defer st.Stop()
	st.SetSleep(true)

	released := make(chan struct{})
	go func() {
		<-st.Wait()
		close(released)
	}()
	time.Sleep(50 * time.Millisecond)

	st.SetSleep(false)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("waiter was not released when sleep mode ended")
	}
}

func TestSleepyTickerSetSleepNoPanic(t *testing.T) {
	t.Parallel()

	// sleepDuration > 0: no channel exists, must not panic.
	st := NewSleepyTicker(time.Hour, time.Hour)
	defer st.Stop()
	st.SetSleep(false)
	st.SetSleep(true)
	st.SetSleep(true)
	st.SetSleep(false)
	st.SetSleep(false)

	// sleepDuration == 0: double calls must not panic or leak waiters.
	st = NewSleepyTicker(time.Hour, 0)
	defer st.Stop()
	st.SetSleep(false)
	st.SetSleep(true)
	ch := st.Wait()
	st.SetSleep(true) // must not replace the channel the waiter holds
	released := make(chan struct{})
	go func() {
		<-ch
		close(released)
	}()
	time.Sleep(50 * time.Millisecond)
	st.SetSleep(false)
	st.SetSleep(false)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("waiter on first channel leaked")
	}
}

func TestSleepyTickerConcurrent(t *testing.T) {
	t.Parallel()

	st := NewSleepyTicker(time.Millisecond, 0)
	defer st.Stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			st.SetSleep(i%2 == 0)
		}
	}()
	for range 200 {
		select {
		case <-st.Wait():
		case <-time.After(time.Millisecond):
		}
	}
	<-done
}

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
