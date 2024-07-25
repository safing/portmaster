package terminal

import (
	"fmt"
	"os"
	"runtime/pprof"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/structures/container"
)

func TestTerminals(t *testing.T) {
	t.Parallel()

	identity, erro := cabin.CreateIdentity(module.mgr.Ctx(), "test")
	if erro != nil {
		t.Fatalf("failed to create identity: %s", erro)
	}

	// Test without and with encryption.
	for _, encrypt := range []bool{false, true} {
		// Test with different flow controls.
		for _, fc := range []struct {
			flowControl     FlowControlType
			flowControlSize uint32
		}{
			{
				flowControl:     FlowControlNone,
				flowControlSize: 5,
			},
			{
				flowControl:     FlowControlDFQ,
				flowControlSize: defaultTestQueueSize,
			},
		} {
			// Run tests with combined options.
			testTerminals(t, identity, &TerminalOpts{
				Encrypt:         encrypt,
				Padding:         defaultTestPadding,
				FlowControl:     fc.flowControl,
				FlowControlSize: fc.flowControlSize,
			})
		}
	}
}

func testTerminals(t *testing.T, identity *cabin.Identity, terminalOpts *TerminalOpts) {
	t.Helper()

	// Prepare encryption.
	var dstHub *hub.Hub
	if terminalOpts.Encrypt {
		dstHub = identity.Hub
	} else {
		identity = nil
	}

	// Create test terminals.
	var term1 *TestTerminal
	var term2 *TestTerminal
	var initData *container.Container
	var err *Error
	term1, initData, err = NewLocalTestTerminal(
		module.mgr.Ctx(), 127, "c1", dstHub, terminalOpts, createForwardingUpstream(
			t, "c1", "c2", func(msg *Msg) *Error {
				return term2.Deliver(msg)
			},
		),
	)
	if err != nil {
		t.Fatalf("failed to create local terminal: %s", err)
	}
	term2, _, err = NewRemoteTestTerminal(
		module.mgr.Ctx(), 127, "c2", identity, initData, createForwardingUpstream(
			t, "c2", "c1", func(msg *Msg) *Error {
				return term1.Deliver(msg)
			},
		),
	)
	if err != nil {
		t.Fatalf("failed to create remote terminal: %s", err)
	}

	// Start testing with counters.
	countToQueueSize := uint64(terminalOpts.FlowControlSize)
	optionsSuffix := fmt.Sprintf(
		"encrypt=%v,flowType=%d",
		terminalOpts.Encrypt,
		terminalOpts.FlowControl,
	)

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlyup-flushing-waiting:" + optionsSuffix,
		flush:           true,
		serverCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: sendThresholdMaxWait * 2,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlyup-waiting:" + optionsSuffix,
		serverCountTo:   10,
		waitBetweenMsgs: sendThresholdMaxWait * 2,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlyup-flushing:" + optionsSuffix,
		flush:           true,
		serverCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: time.Millisecond,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlyup:" + optionsSuffix,
		serverCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: time.Millisecond,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlydown-flushing-waiting:" + optionsSuffix,
		flush:           true,
		clientCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: sendThresholdMaxWait * 2,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlydown-waiting:" + optionsSuffix,
		clientCountTo:   10,
		waitBetweenMsgs: sendThresholdMaxWait * 2,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlydown-flushing:" + optionsSuffix,
		flush:           true,
		clientCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: time.Millisecond,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "onlydown:" + optionsSuffix,
		clientCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: time.Millisecond,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "twoway-flushing-waiting:" + optionsSuffix,
		flush:           true,
		clientCountTo:   countToQueueSize * 2,
		serverCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: sendThresholdMaxWait * 2,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "twoway-waiting:" + optionsSuffix,
		flush:           true,
		clientCountTo:   10,
		serverCountTo:   10,
		waitBetweenMsgs: sendThresholdMaxWait * 2,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "twoway-flushing:" + optionsSuffix,
		flush:           true,
		clientCountTo:   countToQueueSize * 2,
		serverCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: time.Millisecond,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:        "twoway:" + optionsSuffix,
		clientCountTo:   countToQueueSize * 2,
		serverCountTo:   countToQueueSize * 2,
		waitBetweenMsgs: time.Millisecond,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:      "stresstest-down:" + optionsSuffix,
		clientCountTo: countToQueueSize * 1000,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:      "stresstest-up:" + optionsSuffix,
		serverCountTo: countToQueueSize * 1000,
	})

	testTerminalWithCounters(t, term1, term2, &testWithCounterOpts{
		testName:      "stresstest-duplex:" + optionsSuffix,
		clientCountTo: countToQueueSize * 1000,
		serverCountTo: countToQueueSize * 1000,
	})

	// Clean up.
	term1.Abandon(nil)
	term2.Abandon(nil)

	// Give some time for the last log messages and clean up.
	time.Sleep(100 * time.Millisecond)
}

func createForwardingUpstream(t *testing.T, srcName, dstName string, deliverFunc func(*Msg) *Error) Upstream {
	t.Helper()

	return UpstreamSendFunc(func(msg *Msg, _ time.Duration) *Error {
		// Fast track nil containers.
		if msg == nil {
			dErr := deliverFunc(msg)
			if dErr != nil {
				t.Errorf("%s>%s: failed to deliver nil msg to terminal: %s", srcName, dstName, dErr)
				return dErr.With("failed to deliver nil msg to terminal")
			}
			return nil
		}

		// Log messages.
		if logTestCraneMsgs {
			t.Logf("%s>%s: %v\n", srcName, dstName, msg.Data.CompileData())
		}

		// Deliver to other terminal.
		dErr := deliverFunc(msg)
		if dErr != nil {
			t.Errorf("%s>%s: failed to deliver to terminal: %s", srcName, dstName, dErr)
			return dErr.With("failed to deliver to terminal")
		}

		return nil
	})
}

type testWithCounterOpts struct {
	testName        string
	flush           bool
	clientCountTo   uint64
	serverCountTo   uint64
	waitBetweenMsgs time.Duration
}

func testTerminalWithCounters(t *testing.T, term1, term2 *TestTerminal, opts *testWithCounterOpts) {
	t.Helper()

	// Wait async for test to complete, print stack after timeout.
	finished := make(chan struct{})
	maxTestDuration := 60 * time.Second
	go func() {
		select {
		case <-finished:
		case <-time.After(maxTestDuration):
			fmt.Printf("terminal test %s is taking more than %s, printing stack:\n", opts.testName, maxTestDuration)
			_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			os.Exit(1)
		}
	}()

	t.Logf("starting terminal counter test %s", opts.testName)
	defer t.Logf("stopping terminal counter test %s", opts.testName)

	// Start counters.
	counter, tErr := NewCounterOp(term1, CounterOpts{
		ClientCountTo: opts.clientCountTo,
		ServerCountTo: opts.serverCountTo,
		Flush:         opts.flush,
		Wait:          opts.waitBetweenMsgs,
	})
	if tErr != nil {
		t.Fatalf("terminal test %s failed to start counter: %s", opts.testName, tErr)
	}

	// Wait until counters are done.
	counter.Wait()
	close(finished)

	// Check for error.
	if counter.Error != nil {
		t.Fatalf("terminal test %s failed to count: %s", opts.testName, counter.Error)
	}

	// Log stats.
	printCTStats(t, opts.testName, "term1", term1)
	printCTStats(t, opts.testName, "term2", term2)

	// Check if stats match, if DFQ is used on both sides.
	dfq1, ok1 := term1.flowControl.(*DuplexFlowQueue)
	dfq2, ok2 := term2.flowControl.(*DuplexFlowQueue)
	if ok1 && ok2 &&
		(atomic.LoadInt32(dfq1.sendSpace) != atomic.LoadInt32(dfq2.reportedSpace) ||
			atomic.LoadInt32(dfq2.sendSpace) != atomic.LoadInt32(dfq1.reportedSpace)) {
		t.Fatalf("terminal test %s has non-matching space counters", opts.testName)
	}
}

func printCTStats(t *testing.T, testName, name string, term *TestTerminal) {
	t.Helper()

	dfq, ok := term.flowControl.(*DuplexFlowQueue)
	if !ok {
		return
	}

	t.Logf(
		"%s: %s: sq=%d rq=%d sends=%d reps=%d",
		testName,
		name,
		len(dfq.sendQueue),
		len(dfq.recvQueue),
		atomic.LoadInt32(dfq.sendSpace),
		atomic.LoadInt32(dfq.reportedSpace),
	)
}
