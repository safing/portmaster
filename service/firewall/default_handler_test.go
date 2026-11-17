package firewall

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

// verdictTestPacket is a real (not info-only) packet that counts the verdicts
// that are applied to it.
type verdictTestPacket struct {
	packet.Base

	accepted, blocked, dropped             atomic.Uint64
	permAccepted, permBlocked, permDropped atomic.Uint64
	rerouted                               atomic.Uint64

	// layers is returned by Layers, if set. LoadPacketData succeeds then.
	layers gopacket.Packet
}

func newVerdictTestPacket(protocol packet.IPProtocol, inbound bool) *verdictTestPacket {
	pkt := &verdictTestPacket{}
	pkt.SetPacketInfo(packet.Info{
		Inbound:  inbound,
		Version:  packet.IPv4,
		Protocol: protocol,
		Src:      net.IPv4(192, 168, 1, 10),
		SrcPort:  40000,
		Dst:      net.IPv4(1, 1, 1, 1),
		DstPort:  443,
		SeenAt:   time.Now(),
	})
	return pkt
}

func (pkt *verdictTestPacket) Accept() error              { pkt.accepted.Add(1); return nil }
func (pkt *verdictTestPacket) Block() error               { pkt.blocked.Add(1); return nil }
func (pkt *verdictTestPacket) Drop() error                { pkt.dropped.Add(1); return nil }
func (pkt *verdictTestPacket) PermanentAccept() error     { pkt.permAccepted.Add(1); return nil }
func (pkt *verdictTestPacket) PermanentBlock() error      { pkt.permBlocked.Add(1); return nil }
func (pkt *verdictTestPacket) PermanentDrop() error       { pkt.permDropped.Add(1); return nil }
func (pkt *verdictTestPacket) RerouteToNameserver() error { pkt.rerouted.Add(1); return nil }
func (pkt *verdictTestPacket) RerouteToTunnel() error     { pkt.rerouted.Add(1); return nil }
func (pkt *verdictTestPacket) RerouteToSplitTun() error   { pkt.rerouted.Add(1); return nil }

func (pkt *verdictTestPacket) LoadPacketData() error {
	if pkt.layers == nil {
		return packet.ErrFailedToLoadPayload
	}
	return nil
}

func (pkt *verdictTestPacket) Layers() gopacket.Packet { return pkt.layers }

func (pkt *verdictTestPacket) total() uint64 {
	return pkt.accepted.Load() + pkt.blocked.Load() + pkt.dropped.Load() +
		pkt.permAccepted.Load() + pkt.permBlocked.Load() + pkt.permDropped.Load() +
		pkt.rerouted.Load()
}

// setupDefaultHandlerTest prepares the globals that the default firewall
// handler depends on.
func setupDefaultHandlerTest(tb testing.TB, permanent bool) {
	tb.Helper()

	network.SetDefaultFirewallHandler(defaultFirewallHandler, defaultLockFreeFirewallHandler)

	prevPermanentVerdicts := permanentVerdicts
	permanentVerdicts = func() bool { return permanent }
	tb.Cleanup(func() {
		permanentVerdicts = prevPermanentVerdicts
	})
}

// newDecidedConnection returns a connection that is fully decided and has no
// firewall handler, so that its packets are handled by the default handler
// directly on the dispatching goroutine.
func newDecidedConnection(verdict network.Verdict) *network.Connection {
	conn := &network.Connection{
		ID:         "default-handler-test",
		Type:       network.IPConnection,
		IPVersion:  packet.IPv4,
		IPProtocol: packet.TCP,
	}
	conn.Lock()
	conn.SetVerdictDirectly(verdict)
	conn.Unlock()
	return conn
}

// TestDefaultHandlerRacesWithVerdictUpdate checks that packets of decided
// connections, which are handled on the packet dispatching goroutine without
// locking the connection, do not race with a re-evaluation of the connection,
// which changes the verdict state while holding the connection lock.
func TestDefaultHandlerRacesWithVerdictUpdate(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	setupDefaultHandlerTest(t, true)

	const rounds = 2000
	conn := newDecidedConnection(network.VerdictAccept)
	pkt := newVerdictTestPacket(packet.TCP, false)

	var wg sync.WaitGroup

	// Packet dispatcher.
	wg.Go(func() {
		for range rounds {
			conn.HandlePacket(pkt)
		}
	})

	// Verdict re-evaluation, as done by resetConnectionVerdict.
	wg.Go(func() {
		for i := range rounds {
			conn.Lock()
			if i%2 == 0 {
				conn.SetVerdict(network.VerdictBlock, "test", "", nil)
			} else {
				conn.SetVerdict(network.VerdictAccept, "test", "", nil)
			}
			// Also check the lock order: connection lock, then packet queue lock.
			conn.StopFirewallHandler()
			conn.Unlock()
		}
	})

	wg.Wait()

	// Every packet must have received exactly one verdict.
	if pkt.total() != rounds {
		t.Errorf("expected %d verdicts, got %d", rounds, pkt.total())
	}
	if pkt.dropped.Load() != 0 || pkt.permDropped.Load() != 0 || pkt.rerouted.Load() != 0 {
		t.Errorf("unexpected verdicts: %d dropped, %d permanently dropped, %d rerouted",
			pkt.dropped.Load(), pkt.permDropped.Load(), pkt.rerouted.Load())
	}
}

// newICMPEchoTestPacket returns an ICMP echo request packet.
func newICMPEchoTestPacket(t *testing.T, inbound bool) *verdictTestPacket {
	t.Helper()

	buf := gopacket.NewSerializeBuffer()
	err := gopacket.SerializeLayers(buf,
		gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true},
		&layers.IPv4{
			Version:  4,
			IHL:      5,
			TTL:      64,
			Protocol: layers.IPProtocolICMPv4,
			SrcIP:    net.IPv4(192, 168, 1, 10),
			DstIP:    net.IPv4(1, 1, 1, 1),
		},
		&layers.ICMPv4{
			TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeEchoRequest, 0),
			Id:       1,
			Seq:      1,
		},
		gopacket.Payload([]byte("ping")),
	)
	if err != nil {
		t.Fatal(err)
	}

	pkt := newVerdictTestPacket(packet.ICMP, inbound)
	pkt.layers = gopacket.NewPacket(buf.Bytes(), layers.LayerTypeIPv4, gopacket.Default)
	if pkt.layers.ErrorLayer() != nil {
		t.Fatal(pkt.layers.ErrorLayer().Error())
	}
	return pkt
}

// handlePacketWithTimeout handles the packet like the packet dispatcher and
// fails the test if that blocks.
func handlePacketWithTimeout(t *testing.T, conn *network.Connection, pkt packet.Packet) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.HandlePacket(pkt)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("packet dispatching is blocked by the locked connection")
	}
}

// TestDefaultHandlerDoesNotWaitForConnectionLock checks that the packet
// dispatcher is never blocked by a connection that is locked, eg. because it
// is being re-evaluated, and applies the last published verdict instead.
func TestDefaultHandlerDoesNotWaitForConnectionLock(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	setupDefaultHandlerTest(t, false)

	conn := newDecidedConnection(network.VerdictAccept)
	pkt := newVerdictTestPacket(packet.TCP, false)

	// Change the verdict, but keep the connection locked.
	conn.Lock()
	conn.SetVerdictDirectly(network.VerdictBlock)

	handlePacketWithTimeout(t, conn, pkt)
	if pkt.accepted.Load() != 1 || pkt.total() != 1 {
		t.Errorf("expected the published verdict to be applied while the connection is locked, got %d accepted of %d", pkt.accepted.Load(), pkt.total())
	}

	// The new verdict applies as soon as the connection is unlocked.
	conn.Unlock()
	handlePacketWithTimeout(t, conn, pkt)
	if pkt.blocked.Load() != 1 || pkt.total() != 2 {
		t.Errorf("expected the new verdict to be applied after unlocking, got %d blocked of %d", pkt.blocked.Load(), pkt.total())
	}

	// Stopping the firewall handler publishes the verdict state right away,
	// as packets are handled without the lock from then on.
	conn.Lock()
	conn.SetVerdictDirectly(network.VerdictDrop)
	conn.StopFirewallHandler()
	handlePacketWithTimeout(t, conn, pkt)
	conn.Unlock()
	if pkt.dropped.Load() != 1 || pkt.total() != 3 {
		t.Errorf("expected the verdict at the time the handler was stopped, got %d dropped of %d", pkt.dropped.Load(), pkt.total())
	}
}

// TestDefaultHandlerEnablesPermanentVerdict checks that the default handler
// makes the verdict permanent without locking the connection and that the
// connection adopts this.
func TestDefaultHandlerEnablesPermanentVerdict(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	setupDefaultHandlerTest(t, true)

	conn := newDecidedConnection(network.VerdictBlock)
	pkt := newVerdictTestPacket(packet.TCP, false)

	// Handle a packet while the connection is locked and changed.
	conn.Lock()
	conn.SetVerdictDirectly(network.VerdictAccept)
	handlePacketWithTimeout(t, conn, pkt)
	if pkt.permBlocked.Load() != 1 || pkt.total() != 1 {
		t.Errorf("expected a permanent block, got %d of %d", pkt.permBlocked.Load(), pkt.total())
	}
	conn.Unlock()

	// Neither the permanent flag nor the changed verdict may be lost.
	state := conn.VerdictState()
	if !state.VerdictPermanent || state.Verdict != network.VerdictAccept {
		t.Errorf("unexpected verdict state after unlocking: %+v", state)
	}
	conn.Lock()
	if !conn.VerdictPermanent {
		t.Error("connection did not adopt the permanent verdict")
	}
	conn.Unlock()

	handlePacketWithTimeout(t, conn, pkt)
	if pkt.permAccepted.Load() != 1 || pkt.total() != 2 {
		t.Errorf("expected a permanent accept, got %d of %d", pkt.permAccepted.Load(), pkt.total())
	}

	// Info-only packets never get a verdict.
	handlePacketWithTimeout(t, conn, packet.NewInfoPacket(*pkt.Info()))
	if pkt.total() != 2 {
		t.Errorf("unexpected verdict count %d", pkt.total())
	}
}

// TestDefaultHandlerICMP checks the handling of ICMP packets of connections
// without a firewall handler.
func TestDefaultHandlerICMP(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	setupDefaultHandlerTest(t, true)

	// Create an inbound ICMP connection that was accepted, eg. by fast-tracking.
	inboundPkt := newICMPEchoTestPacket(t, true)
	conn := network.NewIncompleteConnection(inboundPkt)
	conn.Lock()
	conn.SetVerdictDirectly(network.VerdictAccept)
	conn.StopFirewallHandler()
	conn.Unlock()
	if !conn.VerdictState().Inbound {
		t.Fatal("connection must be inbound")
	}

	// Packets that cannot be loaded are dropped.
	brokenPkt := newVerdictTestPacket(packet.ICMP, true)
	handlePacketWithTimeout(t, conn, brokenPkt)
	if brokenPkt.dropped.Load() != 1 || brokenPkt.total() != 1 {
		t.Errorf("expected broken packet to be dropped, got %d of %d", brokenPkt.dropped.Load(), brokenPkt.total())
	}

	// Inbound packets get the verdict of the connection, never a permanent one.
	handlePacketWithTimeout(t, conn, inboundPkt)
	if inboundPkt.accepted.Load() != 1 || inboundPkt.total() != 1 {
		t.Errorf("expected inbound packet to be accepted, got %d of %d", inboundPkt.accepted.Load(), inboundPkt.total())
	}

	// An outbound packet changes the direction of the connection. This needs
	// the connection lock, so it is skipped while the connection is locked.
	outboundPkt := newICMPEchoTestPacket(t, false)
	conn.Lock()
	handlePacketWithTimeout(t, conn, outboundPkt)
	conn.Unlock()
	if outboundPkt.accepted.Load() != 1 || outboundPkt.total() != 1 {
		t.Errorf("expected outbound packet to be accepted, got %d of %d", outboundPkt.accepted.Load(), outboundPkt.total())
	}
	if !conn.VerdictState().Inbound {
		t.Error("direction must not change while the connection is locked")
	}

	// The next outbound packet changes the direction.
	handlePacketWithTimeout(t, conn, outboundPkt)
	if outboundPkt.accepted.Load() != 2 || outboundPkt.total() != 2 {
		t.Errorf("expected outbound packet to be accepted, got %d of %d", outboundPkt.accepted.Load(), outboundPkt.total())
	}
	if conn.VerdictState().Inbound {
		t.Error("direction did not change to outbound")
	}
	conn.Lock()
	if conn.Inbound || conn.VerdictPermanent {
		t.Errorf("unexpected connection state: inbound=%v permanent=%v", conn.Inbound, conn.VerdictPermanent)
	}
	conn.Unlock()
}

// TestDefaultHandlerICMPRacesWithConnectionUpdate checks that ICMP packets
// handled by the default handler do not race with users of the connection.
func TestDefaultHandlerICMPRacesWithConnectionUpdate(t *testing.T) { //nolint:paralleltest // Modifies package globals.
	setupDefaultHandlerTest(t, true)

	const rounds = 2000
	inboundPkt := newICMPEchoTestPacket(t, true)
	inboundPkt.Info().SrcPort = 1 // Use a distinct connection ID.
	outboundPkt := newICMPEchoTestPacket(t, false)
	conn := network.NewIncompleteConnection(inboundPkt)
	conn.Lock()
	conn.SetVerdictDirectly(network.VerdictAccept)
	conn.StopFirewallHandler()
	conn.Unlock()

	var wg sync.WaitGroup
	wg.Go(func() {
		for range rounds {
			conn.HandlePacket(outboundPkt)
		}
	})
	wg.Go(func() {
		for i := range rounds {
			conn.Lock()
			// Flip the direction back and re-evaluate.
			conn.Inbound = true
			if i%2 == 0 {
				conn.SetVerdict(network.VerdictBlock, "test", "", nil)
			} else {
				conn.SetVerdict(network.VerdictAccept, "test", "", nil)
			}
			conn.Unlock()
		}
	})
	wg.Wait()

	if outboundPkt.total() != rounds {
		t.Errorf("expected %d verdicts, got %d", rounds, outboundPkt.total())
	}
}

func BenchmarkDefaultHandlerPacketPath(b *testing.B) {
	setupDefaultHandlerTest(b, true)

	conn := newDecidedConnection(network.VerdictAccept)
	pkt := newVerdictTestPacket(packet.TCP, false)
	conn.HandlePacket(pkt)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		conn.HandlePacket(pkt)
	}
}
