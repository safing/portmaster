//go:build linux

package nfq

import (
	"context"
	"testing"
	"time"

	pmpacket "github.com/safing/portmaster/service/network/packet"
)

// setWatchdogForTest shortens the verdict timeout and replaces the timeout
// handler with one that reports the packet on the returned channel.
func setWatchdogForTest(t *testing.T, timeout time.Duration) <-chan *packet {
	t.Helper()

	fired := make(chan *packet, 10)
	oldTimeout, oldHandler := verdictTimeout, verdictTimeoutHandler
	verdictTimeout = timeout
	verdictTimeoutHandler = func(pkt *packet) { fired <- pkt }
	t.Cleanup(func() {
		verdictTimeout, verdictTimeoutHandler = oldTimeout, oldHandler
	})
	return fired
}

func TestVerdictWatchdogStoppedByVerdict(t *testing.T) { //nolint:paralleltest // Modifies the package-level verdict timeout and handler.
	fired := setWatchdogForTest(t, 50*time.Millisecond)

	q := newTestQueue()
	pkt := newTestPacket(q, 1)
	q.queuePacket(context.Background(), pkt)

	if queued := <-q.packets; queued != pkt {
		t.Fatal("unexpected packet in queue")
	}
	if pkt.verdictWatchdog.Load() == nil {
		t.Fatal("watchdog not armed for queued packet")
	}
	simulateVerdict(pkt)

	// The watchdog must have been stopped by setting the verdict.
	if pkt.verdictWatchdog.Load().Stop() {
		t.Fatal("watchdog still active after verdict was set")
	}
	select {
	case <-fired:
		t.Fatal("watchdog fired although a verdict was set")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestVerdictWatchdogVerdictBeforeArming(t *testing.T) { //nolint:paralleltest // Modifies the package-level verdict timeout and handler.
	fired := setWatchdogForTest(t, 50*time.Millisecond)

	// The verdict is set by a handler before the receive goroutine armed the
	// watchdog: arming must stop it right away.
	pkt := newTestPacket(newTestQueue(), 1)
	simulateVerdict(pkt)
	pkt.startVerdictWatchdog()

	if pkt.verdictWatchdog.Load().Stop() {
		t.Fatal("watchdog still active after verdict was set")
	}
	select {
	case <-fired:
		t.Fatal("watchdog fired although a verdict was set")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestVerdictWatchdogFiresWithoutVerdict(t *testing.T) { //nolint:paralleltest // Modifies the package-level verdict timeout and handler.
	const timeout = 100 * time.Millisecond
	fired := setWatchdogForTest(t, timeout)

	q := newTestQueue()
	pkt := newTestPacket(q, 1)
	start := time.Now()
	q.queuePacket(context.Background(), pkt)
	<-q.packets // The handler receives the packet, but never sets a verdict.

	select {
	case got := <-fired:
		if got != pkt {
			t.Fatal("watchdog fired for wrong packet")
		}
		if elapsed := time.Since(start); elapsed < timeout {
			t.Fatalf("watchdog fired too early: after %s", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not fire for packet without verdict")
	}
	select {
	case <-fired:
		t.Fatal("watchdog fired more than once")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHandleVerdictTimeoutAfterVerdict(t *testing.T) {
	t.Parallel()

	// If the verdict was set by the time the watchdog fires, the timeout
	// handler must neither log nor try to drop (which would need a live queue
	// and panic without one).
	pkt := newTestPacket(newTestQueue(), 1)
	simulateVerdict(pkt)
	pkt.handleVerdictTimeout()
}

func TestQueuePacketCanceled(t *testing.T) { //nolint:paralleltest // Modifies the package-level verdict timeout and handler.
	fired := setWatchdogForTest(t, 50*time.Millisecond)

	// Full handler queue and canceled context: the packet is not queued and
	// no watchdog is armed, as before.
	q := newTestQueue()
	q.packets = make(chan pmpacket.Packet)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pkt := newTestPacket(q, 1)
	q.queuePacket(ctx, pkt)
	if pkt.verdictWatchdog.Load() != nil {
		t.Fatal("watchdog armed for packet that was not queued")
	}
	select {
	case <-fired:
		t.Fatal("watchdog fired for packet that was not queued")
	case <-time.After(200 * time.Millisecond):
	}
}
