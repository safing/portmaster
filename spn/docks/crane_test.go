package docks

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
)

func TestCraneCommunication(t *testing.T) {
	t.Parallel()

	testCraneWithCounter(t, "plain-counter-load-100", false, 100, 1000)
	testCraneWithCounter(t, "plain-counter-load-1000", false, 1000, 1000)
	testCraneWithCounter(t, "plain-counter-load-10000", false, 10000, 1000)
	testCraneWithCounter(t, "encrypted-counter", true, 1000, 1000)
}

func testCraneWithCounter(t *testing.T, testID string, encrypting bool, loadSize int, countTo uint64) { //nolint:unparam,thelper
	var identity *cabin.Identity
	var connectedHub *hub.Hub
	if encrypting {
		identity, connectedHub = getTestIdentity(t)
	}

	// Build ship and cranes.
	optimalMinLoadSize = loadSize * 2
	ship := ships.NewTestShip(!encrypting, loadSize)

	var crane1, crane2 *Crane
	var craneWg sync.WaitGroup
	craneWg.Add(2)

	go func() {
		var err error
		crane1, err = NewCrane(ship, connectedHub, nil)
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not create crane1: %s", testID, err))
		}
		err = crane1.Start(module.mgr.Ctx())
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not start crane1: %s", testID, err))
		}
		craneWg.Done()
	}()
	go func() {
		var err error
		crane2, err = NewCrane(ship.Reverse(), nil, identity)
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not create crane2: %s", testID, err))
		}
		err = crane2.Start(module.mgr.Ctx())
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not start crane2: %s", testID, err))
		}
		craneWg.Done()
	}()

	craneWg.Wait()
	t.Logf("crane test %s setup complete", testID)

	// Wait async for test to complete, print stack after timeout.
	finished := make(chan struct{})
	go func() {
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Logf("crane test %s is taking too long, print stack:", testID)
			_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			os.Exit(1)
		}
	}()

	t.Logf("crane1 controller: %+v", crane1.Controller)
	t.Logf("crane2 controller: %+v", crane2.Controller)

	// Start counters for testing.
	op1, tErr := terminal.NewCounterOp(crane1.Controller, terminal.CounterOpts{
		ClientCountTo: countTo,
		ServerCountTo: countTo,
	})
	if tErr != nil {
		t.Fatalf("crane test %s failed to run counter op: %s", testID, tErr)
	}

	// Wait for completion.
	op1.Wait()
	close(finished)

	// Wait a little so that all errors can be propagated, so we can truly see
	// if we succeeded.
	time.Sleep(1 * time.Second)

	// Check errors.
	if op1.Error != nil {
		t.Fatalf("crane test %s counter op1 failed: %s", testID, op1.Error)
	}
}

type StreamingTerminal struct {
	terminal.BareTerminal

	test     *testing.T
	id       uint32
	crane    *Crane
	recv     chan *terminal.Msg
	testData []byte
}

func (t *StreamingTerminal) ID() uint32 {
	return t.id
}

func (t *StreamingTerminal) Ctx() context.Context {
	return module.mgr.Ctx()
}

func (t *StreamingTerminal) Deliver(msg *terminal.Msg) *terminal.Error {
	t.recv <- msg
	msg.Finish()
	return nil
}

func (t *StreamingTerminal) Abandon(err *terminal.Error) {
	t.crane.AbandonTerminal(t.ID(), err)
	if err != nil {
		t.test.Errorf("streaming terminal %d failed: %s", t.id, err)
	}
}

func (t *StreamingTerminal) FmtID() string {
	return fmt.Sprintf("test-%d", t.id)
}

func TestCraneLoadingUnloading(t *testing.T) {
	t.Parallel()

	testCraneWithStreaming(t, "plain-streaming", false, 100)
	testCraneWithStreaming(t, "encrypted-streaming", true, 100)
}

func testCraneWithStreaming(t *testing.T, testID string, encrypting bool, loadSize int) { //nolint:thelper
	var identity *cabin.Identity
	var connectedHub *hub.Hub
	if encrypting {
		identity, connectedHub = getTestIdentity(t)
	}

	// Build ship and cranes.
	optimalMinLoadSize = loadSize * 2
	ship := ships.NewTestShip(!encrypting, loadSize)

	var crane1, crane2 *Crane
	var craneWg sync.WaitGroup
	craneWg.Add(2)

	go func() {
		var err error
		crane1, err = NewCrane(ship, connectedHub, nil)
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not create crane1: %s", testID, err))
		}
		err = crane1.Start(module.mgr.Ctx())
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not start crane1: %s", testID, err))
		}
		craneWg.Done()
	}()
	go func() {
		var err error
		crane2, err = NewCrane(ship.Reverse(), nil, identity)
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not create crane2: %s", testID, err))
		}
		err = crane2.Start(module.mgr.Ctx())
		if err != nil {
			panic(fmt.Sprintf("crane test %s could not start crane2: %s", testID, err))
		}
		craneWg.Done()
	}()

	craneWg.Wait()
	t.Logf("crane test %s setup complete", testID)

	// Wait async for test to complete, print stack after timeout.
	finished := make(chan struct{})
	go func() {
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Logf("crane test %s is taking too long, print stack:", testID)
			_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			os.Exit(1)
		}
	}()

	t.Logf("crane1 controller: %+v", crane1.Controller)
	t.Logf("crane2 controller: %+v", crane2.Controller)

	// Create terminals and run test.
	st := &StreamingTerminal{
		test:     t,
		id:       8,
		crane:    crane2,
		recv:     make(chan *terminal.Msg),
		testData: []byte("The quick brown fox jumps over the lazy dog."),
	}
	crane2.terminals[st.ID()] = st

	// Run streaming test.
	var streamingWg sync.WaitGroup
	streamingWg.Add(2)
	count := 10000
	go func() {
		for i := 1; i <= count; i++ {
			msg := terminal.NewMsg(st.testData)
			msg.FlowID = st.id
			err := crane1.Send(msg, 1*time.Second)
			if err != nil {
				msg.Finish()
				crane1.Stop(err.Wrap("failed to submit terminal msg"))
			}
			// log.Tracef("spn/testing: + %d", i)
		}
		t.Logf("crane test %s done with sending", testID)
		streamingWg.Done()
	}()
	go func() {
		for i := 1; i <= count; i++ {
			msg := <-st.recv
			assert.Equal(t, st.testData, msg.Data.CompileData(), "data mismatched")
			// log.Tracef("spn/testing: - %d", i)
		}
		t.Logf("crane test %s done with receiving", testID)
		streamingWg.Done()
	}()

	// Wait for completion.
	streamingWg.Wait()
	close(finished)
}

var testIdentity *cabin.Identity

func getTestIdentity(t *testing.T) (*cabin.Identity, *hub.Hub) {
	t.Helper()

	if testIdentity == nil {
		var err error
		testIdentity, err = cabin.CreateIdentity(module.mgr.Ctx(), "test")
		if err != nil {
			t.Fatalf("failed to create identity: %s", err)
		}
	}

	return testIdentity, testIdentity.Hub
}
