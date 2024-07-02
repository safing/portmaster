package rng

import (
	"testing"
	"time"
)

func TestFeeder(t *testing.T) {
	t.Parallel()

	// wait for start / first round to complete
	time.Sleep(1 * time.Millisecond)

	f := NewFeeder()

	// go through all functions
	f.NeedsEntropy()
	f.SupplyEntropy([]byte{0}, 0)
	f.SupplyEntropyAsInt(0, 0)
	f.SupplyEntropyIfNeeded([]byte{0}, 0)
	f.SupplyEntropyAsIntIfNeeded(0, 0)

	// fill entropy
	f.SupplyEntropyAsInt(0, 65535)

	// check blocking calls

	waitOne := make(chan struct{})
	go func() {
		f.SupplyEntropy([]byte{0}, 0)
		close(waitOne)
	}()
	select {
	case <-waitOne:
		t.Error("call does not block!")
	case <-time.After(10 * time.Millisecond):
	}

	waitTwo := make(chan struct{})
	go func() {
		f.SupplyEntropyAsInt(0, 0)
		close(waitTwo)
	}()
	select {
	case <-waitTwo:
		t.Error("call does not block!")
	case <-time.After(10 * time.Millisecond):
	}

	// check non-blocking calls

	waitThree := make(chan struct{})
	go func() {
		f.SupplyEntropyIfNeeded([]byte{0}, 0)
		close(waitThree)
	}()
	select {
	case <-waitThree:
	case <-time.After(10 * time.Millisecond):
		t.Error("call blocks!")
	}

	waitFour := make(chan struct{})
	go func() {
		f.SupplyEntropyAsIntIfNeeded(0, 0)
		close(waitFour)
	}()
	select {
	case <-waitFour:
	case <-time.After(10 * time.Millisecond):
		t.Error("call blocks!")
	}
}
