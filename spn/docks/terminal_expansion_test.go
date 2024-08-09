package docks

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
)

const defaultTestQueueSize = 200

func TestExpansion(t *testing.T) {
	t.Parallel()

	// Test without and with encryption.
	for _, encrypt := range []bool{false, true} {
		// Test down/up separately and in parallel.
		for _, parallel := range []bool{false, true} {
			// Test with different flow controls.
			for _, fc := range []struct {
				flowControl     terminal.FlowControlType
				flowControlSize uint32
			}{
				{
					flowControl:     terminal.FlowControlNone,
					flowControlSize: 5,
				},
				{
					flowControl:     terminal.FlowControlDFQ,
					flowControlSize: defaultTestQueueSize,
				},
			} {
				// Run tests with combined options.
				testExpansion(
					t,
					"expansion-hop-test",
					&terminal.TerminalOpts{
						Encrypt:         encrypt,
						Padding:         8,
						FlowControl:     fc.flowControl,
						FlowControlSize: fc.flowControlSize,
					},
					defaultTestQueueSize,
					defaultTestQueueSize,
					parallel,
				)
			}
		}
	}

	stressTestOpts := &terminal.TerminalOpts{
		Encrypt:         true,
		Padding:         8,
		FlowControl:     terminal.FlowControlDFQ,
		FlowControlSize: defaultTestQueueSize,
	}
	testExpansion(t, "expansion-stress-test-down", stressTestOpts, defaultTestQueueSize*100, 0, false)
	testExpansion(t, "expansion-stress-test-up", stressTestOpts, 0, defaultTestQueueSize*100, false)
	testExpansion(t, "expansion-stress-test-duplex", stressTestOpts, defaultTestQueueSize*100, defaultTestQueueSize*100, false)
}

func testExpansion( //nolint:maintidx,thelper
	t *testing.T,
	testID string,
	terminalOpts *terminal.TerminalOpts,
	clientCountTo,
	serverCountTo uint64,
	inParallel bool,
) {
	testID += fmt.Sprintf(":encrypt=%v,flowCtrl=%d,parallel=%v", terminalOpts.Encrypt, terminalOpts.FlowControl, inParallel)

	var identity2, identity3, identity4 *cabin.Identity
	var connectedHub2, connectedHub3, connectedHub4 *hub.Hub
	if terminalOpts.Encrypt {
		identity2, connectedHub2 = getTestIdentity(t)
		identity3, connectedHub3 = getTestIdentity(t)
		identity4, connectedHub4 = getTestIdentity(t)
	}

	// Build ships and cranes.
	optimalMinLoadSize = 100
	ship1to2 := ships.NewTestShip(!terminalOpts.Encrypt, 100)
	ship2to3 := ships.NewTestShip(!terminalOpts.Encrypt, 100)
	ship3to4 := ships.NewTestShip(!terminalOpts.Encrypt, 100)

	var crane1, crane2to1, crane2to3, crane3to2, crane3to4, crane4 *Crane
	var craneWg sync.WaitGroup
	craneWg.Add(6)

	craneCtx, cancelCraneCtx := context.WithCancel(context.Background())
	defer cancelCraneCtx()

	go func() {
		var err error
		crane1, err = NewCrane(ship1to2, connectedHub2, nil)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not create crane1: %s", testID, err))
		}
		crane1.ID = "c1"
		err = crane1.Start(craneCtx)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not start crane1: %s", testID, err))
		}
		crane1.ship.MarkPublic()
		craneWg.Done()
	}()
	go func() {
		var err error
		crane2to1, err = NewCrane(ship1to2.Reverse(), nil, identity2)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not create crane2to1: %s", testID, err))
		}
		crane2to1.ID = "c2to1"
		err = crane2to1.Start(craneCtx)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not start crane2to1: %s", testID, err))
		}
		crane2to1.ship.MarkPublic()
		craneWg.Done()
	}()
	go func() {
		var err error
		crane2to3, err = NewCrane(ship2to3, connectedHub3, nil)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not create crane2to3: %s", testID, err))
		}
		crane2to3.ID = "c2to3"
		err = crane2to3.Start(craneCtx)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not start crane2to3: %s", testID, err))
		}
		crane2to3.ship.MarkPublic()
		craneWg.Done()
	}()
	go func() {
		var err error
		crane3to2, err = NewCrane(ship2to3.Reverse(), nil, identity3)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not create crane3to2: %s", testID, err))
		}
		crane3to2.ID = "c3to2"
		err = crane3to2.Start(craneCtx)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not start crane3to2: %s", testID, err))
		}
		crane3to2.ship.MarkPublic()
		craneWg.Done()
	}()
	go func() {
		var err error
		crane3to4, err = NewCrane(ship3to4, connectedHub4, nil)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not create crane3to4: %s", testID, err))
		}
		crane3to4.ID = "c3to4"
		err = crane3to4.Start(craneCtx)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not start crane3to4: %s", testID, err))
		}
		crane3to4.ship.MarkPublic()
		craneWg.Done()
	}()
	go func() {
		var err error
		crane4, err = NewCrane(ship3to4.Reverse(), nil, identity4)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not create crane4: %s", testID, err))
		}
		crane4.ID = "c4"
		err = crane4.Start(craneCtx)
		if err != nil {
			panic(fmt.Sprintf("expansion test %s could not start crane4: %s", testID, err))
		}
		crane4.ship.MarkPublic()
		craneWg.Done()
	}()
	craneWg.Wait()

	// Assign cranes.
	crane3HubID := testID + "-crane3HubID"
	AssignCrane(crane3HubID, crane2to3)
	crane4HubID := testID + "-crane4HubID"
	AssignCrane(crane4HubID, crane3to4)

	t.Logf("expansion test %s: initial setup complete", testID)

	// Wait async for test to complete, print stack after timeout.
	finished := make(chan struct{})
	go func() {
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			fmt.Printf("expansion test %s is taking too long, print stack:\n", testID)
			_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			os.Exit(1)
		}
	}()

	// Start initial crane.
	homeTerminal, initData, tErr := NewLocalCraneTerminal(crane1, nil, &terminal.TerminalOpts{})
	if tErr != nil {
		t.Fatalf("expansion test %s failed to create home terminal: %s", testID, tErr)
	}
	tErr = crane1.EstablishNewTerminal(homeTerminal, initData)
	if tErr != nil {
		t.Fatalf("expansion test %s failed to connect home terminal: %s", testID, tErr)
	}

	t.Logf("expansion test %s: home terminal setup complete", testID)
	time.Sleep(100 * time.Millisecond)

	// Start counters for testing.
	op0, tErr := terminal.NewCounterOp(homeTerminal, terminal.CounterOpts{
		ClientCountTo: clientCountTo,
		ServerCountTo: serverCountTo,
	})
	if tErr != nil {
		t.Fatalf("expansion test %s failed to run counter op: %s", testID, tErr)
	}
	t.Logf("expansion test %s: home terminal counter setup complete", testID)
	if !inParallel {
		op0.Wait()
	}

	// Start expansion to crane 3.
	opAuthTo2, tErr := access.AuthorizeToTerminal(homeTerminal)
	if tErr != nil {
		t.Fatalf("expansion test %s failed to auth with home terminal: %s", testID, tErr)
	}
	tErr = <-opAuthTo2.Result
	if tErr.IsError() {
		t.Fatalf("expansion test %s failed to auth with home terminal: %s", testID, tErr)
	}
	expansionTerminalTo3, err := ExpandTo(homeTerminal, crane3HubID, connectedHub3)
	if err != nil {
		t.Fatalf("expansion test %s failed to expand to %s: %s", testID, crane3HubID, tErr)
	}

	// Start counters for testing.
	op1, tErr := terminal.NewCounterOp(expansionTerminalTo3, terminal.CounterOpts{
		ClientCountTo: clientCountTo,
		ServerCountTo: serverCountTo,
	})
	if tErr != nil {
		t.Fatalf("expansion test %s failed to run counter op: %s", testID, tErr)
	}

	t.Logf("expansion test %s: expansion to crane3 and counter setup complete", testID)
	if !inParallel {
		op1.Wait()
	}

	// Start expansion to crane 4.
	opAuthTo3, tErr := access.AuthorizeToTerminal(expansionTerminalTo3)
	if tErr != nil {
		t.Fatalf("expansion test %s failed to auth with extenstion terminal: %s", testID, tErr)
	}
	tErr = <-opAuthTo3.Result
	if tErr.IsError() {
		t.Fatalf("expansion test %s failed to auth with extenstion terminal: %s", testID, tErr)
	}

	expansionTerminalTo4, err := ExpandTo(expansionTerminalTo3, crane4HubID, connectedHub4)
	if err != nil {
		t.Fatalf("expansion test %s failed to expand to %s: %s", testID, crane4HubID, tErr)
	}

	// Start counters for testing.
	op2, tErr := terminal.NewCounterOp(expansionTerminalTo4, terminal.CounterOpts{
		ClientCountTo: clientCountTo,
		ServerCountTo: serverCountTo,
	})
	if tErr != nil {
		t.Fatalf("expansion test %s failed to run counter op: %s", testID, tErr)
	}

	t.Logf("expansion test %s: expansion to crane4 and counter setup complete", testID)
	op2.Wait()

	// Wait for op1 if not already.
	if inParallel {
		op0.Wait()
		op1.Wait()
	}

	// Signal completion.
	close(finished)

	// Wait a little so that all errors can be propagated, so we can truly see
	// if we succeeded.
	time.Sleep(100 * time.Millisecond)

	// Check errors.
	if op1.Error != nil {
		t.Fatalf("crane test %s counter op1 failed: %s", testID, op1.Error)
	}
	if op2.Error != nil {
		t.Fatalf("crane test %s counter op2 failed: %s", testID, op2.Error)
	}
}
