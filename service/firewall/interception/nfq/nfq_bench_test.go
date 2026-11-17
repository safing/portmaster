//go:build linux

package nfq

import (
	"context"
	"testing"

	"github.com/florianl/go-nfqueue"
	"github.com/tevino/abool"

	pmpacket "github.com/safing/portmaster/service/network/packet"
)

// simulateVerdict does what packet.mark does, minus talking to the kernel.
func simulateVerdict(pkt *packet) {
	if pkt.verdictPending.SetToIf(false, true) {
		pkt.verdictDone()
	}
}

func newTestQueue() *Queue {
	return &Queue{
		id:               1,
		packets:          make(chan pmpacket.Packet, 1000),
		verdictCompleted: make(chan struct{}, 1),
	}
}

func newTestPacket(q *Queue, id uint32) *packet {
	return &packet{
		pktID:          id,
		queue:          q,
		verdictSet:     make(chan struct{}),
		verdictPending: abool.New(),
	}
}

// runVerdicter consumes packets from the queue and sets a verdict on each.
func runVerdicter(q *Queue) (stop func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for p := range q.packets {
			simulateVerdict(p.(*packet)) //nolint:forcetypeassert
		}
	}()
	return func() {
		close(q.packets)
		<-done
	}
}

// BenchmarkQueuePacket measures the per-packet bookkeeping done on the nfqueue
// receive goroutine after parsing: handing the packet to the handler queue and
// arming the missing-verdict watchdog. A consumer sets verdicts immediately.
func BenchmarkQueuePacket(b *testing.B) {
	q := newTestQueue()
	stop := runVerdicter(q)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		q.queuePacket(ctx, newTestPacket(q, uint32(i))) //nolint:gosec
	}
	b.StopTimer()
	stop()
}

// testPayload is a minimal IPv4/TCP SYN packet (10.0.0.1:40000 -> 10.0.0.2:443).
var testPayload = []byte{
	0x45, 0x00, 0x00, 0x28, 0x00, 0x01, 0x40, 0x00, 0x40, 0x06, 0x00, 0x00,
	10, 0, 0, 1, 10, 0, 0, 2,
	0x9c, 0x40, 0x01, 0xbb, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
	0x50, 0x02, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00,
}

// BenchmarkPacketHandler measures the complete nfqueue callback (including
// payload parsing) for context.
func BenchmarkPacketHandler(b *testing.B) {
	q := newTestQueue()
	stop := runVerdicter(q)
	handler := q.packetHandler(context.Background())
	payload := testPayload

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		id := uint32(i) //nolint:gosec
		handler(nfqueue.Attribute{PacketID: &id, Payload: &payload})
	}
	b.StopTimer()
	stop()
}
