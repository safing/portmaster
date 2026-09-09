package network

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
)

func resetPIDHints() {
	pidHints.lock.Lock()
	defer pidHints.lock.Unlock()

	pidHints.cur = make(map[string]int, 8)
	pidHints.prev = make(map[string]int, 8)
}

func countPIDHints() int {
	pidHints.lock.Lock()
	defer pidHints.lock.Unlock()

	return len(pidHints.cur) + len(pidHints.prev)
}

func testPacketInfo(srcPort, dstPort uint16, pid int, inbound bool) packet.Info {
	return packet.Info{
		Inbound:  inbound,
		Version:  packet.IPv4,
		Protocol: packet.TCP,
		Src:      net.IPv4(10, 0, 0, 1),
		SrcPort:  srcPort,
		Dst:      net.IPv4(93, 184, 216, 34),
		DstPort:  dstPort,
		PID:      pid,
		SeenAt:   time.Now(),
	}
}

// testRealPacket wraps an info packet, but reports itself as a real packet.
type testRealPacket struct {
	*packet.InfoPacket
}

func (pkt *testRealPacket) InfoOnly() bool {
	return false
}

func newTestRealPacket(info packet.Info) *testRealPacket {
	return &testRealPacket{InfoPacket: packet.NewInfoPacket(info)}
}

func TestPIDHintStoreAndTake(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	pkt := packet.NewInfoPacket(testPacketInfo(10000, 443, 4242, false))
	SavePIDHint(pkt)

	pid, ok := takePIDHint(pkt.GetConnectionID())
	if !ok {
		t.Fatal("expected to find pid hint")
	}
	if pid != 4242 {
		t.Errorf("expected pid 4242, got %d", pid)
	}

	// Hints are consumed on take.
	if _, ok := takePIDHint(pkt.GetConnectionID()); ok {
		t.Error("expected pid hint to be removed after taking it")
	}
}

func TestPIDHintRotation(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	pkt := packet.NewInfoPacket(testPacketInfo(10001, 443, 4243, false))
	SavePIDHint(pkt)

	// One rotation moves the hint to the previous generation, where it is still found.
	pidHints.lock.Lock()
	rotatePIDHints()
	pidHints.lock.Unlock()

	if _, ok := takePIDHint(pkt.GetConnectionID()); !ok {
		t.Fatal("expected pid hint to survive one rotation")
	}

	// Two rotations drop the hint.
	SavePIDHint(pkt)
	pidHints.lock.Lock()
	rotatePIDHints()
	rotatePIDHints()
	pidHints.lock.Unlock()

	if _, ok := takePIDHint(pkt.GetConnectionID()); ok {
		t.Error("expected pid hint to be dropped after two rotations")
	}
}

func TestPIDHintMaxEntries(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	for i := range pidHintMaxEntries + 10 {
		SavePIDHint(packet.NewInfoPacket(testPacketInfo(uint16(10000+i), 443, 1000+i, false))) //nolint:gosec // Test data.
	}

	if cnt := countPIDHints(); cnt > 2*pidHintMaxEntries {
		t.Errorf("expected pid hints to stay bounded, got %d", cnt)
	}
}

func TestPIDHintIgnoresUnusablePackets(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	// Real packets must not create hints.
	realPkt := newTestRealPacket(testPacketInfo(10100, 443, 4244, false))
	SavePIDHint(realPkt)
	if _, ok := takePIDHint(realPkt.GetConnectionID()); ok {
		t.Error("expected no pid hint for a real packet")
	}

	// Info-only packets without a pid have nothing to offer.
	noPID := packet.NewInfoPacket(testPacketInfo(10101, 443, process.UndefinedProcessID, false))
	SavePIDHint(noPID)
	if _, ok := takePIDHint(noPID.GetConnectionID()); ok {
		t.Error("expected no pid hint for an info-only packet without a pid")
	}
}

func TestPIDHintSetsInfoOnlyPacketsActive(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()
	infoOnlyPacketsActive.UnSet()

	SavePIDHint(packet.NewInfoPacket(testPacketInfo(10200, 443, 4245, false)))

	if !infoOnlyPacketsActive.IsSet() {
		t.Error("expected info-only packets to be marked as active")
	}

	// The flag describes the transport, not the payload, so it is also set for
	// info-only packets that carry no usable pid.
	infoOnlyPacketsActive.UnSet()
	SavePIDHint(packet.NewInfoPacket(testPacketInfo(10201, 443, process.UndefinedProcessID, false)))

	if !infoOnlyPacketsActive.IsSet() {
		t.Error("expected info-only packets without a pid to be marked as active")
	}
}

// TestPIDHintConcurrency guards the locking discipline of the pid hint cache and
// is only meaningful when run with -race. It targets two mistakes that are easy
// to introduce later: taking a hint mutates the map, so takePIDHint must hold
// the full lock and not an RLock; and rotation replaces the maps, so no caller
// may hold on to a map reference outside the lock.
func TestPIDHintConcurrency(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				pkt := packet.NewInfoPacket(testPacketInfo(uint16(20000+worker*500+i), 443, 5000+i, false)) //nolint:gosec // Test data.
				SavePIDHint(pkt)
				_, _ = takePIDHint(pkt.GetConnectionID())
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			pidHints.lock.Lock()
			rotatePIDHints()
			pidHints.lock.Unlock()
		}
	}()

	wg.Wait()
}

func TestIncompleteConnectionAdoptsPIDHint(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	info := testPacketInfo(10300, 443, 4246, false)
	SavePIDHint(packet.NewInfoPacket(info))

	// The real packet does not know the pid.
	realInfo := info
	realInfo.PID = process.UndefinedProcessID
	realPkt := newTestRealPacket(realInfo)

	conn := NewIncompleteConnection(realPkt)
	defer conns.delete(conn)

	if conn.PID != 4246 {
		t.Errorf("expected pid 4246 from hint, got %d", conn.PID)
	}
}

func TestIncompleteConnectionKeepsPacketPID(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	info := testPacketInfo(10400, 443, 4247, false)
	SavePIDHint(packet.NewInfoPacket(info))

	// The real packet knows the pid itself and must not be overridden.
	realInfo := info
	realInfo.PID = 9999
	realPkt := newTestRealPacket(realInfo)

	conn := NewIncompleteConnection(realPkt)
	defer conns.delete(conn)

	if conn.PID != 9999 {
		t.Errorf("expected pid 9999 from packet, got %d", conn.PID)
	}
}

func TestIncompleteConnectionKeepsPacketDirection(t *testing.T) { //nolint:paralleltest // Mutates package state.
	resetPIDHints()

	// The eBPF listener only ever reports outbound connections.
	outbound := testPacketInfo(10500, 443, 4249, false)
	SavePIDHint(packet.NewInfoPacket(outbound))

	// The same connection, but intercepted on the inbound hook. The connection ID
	// is identical, because it is normalized to local-first.
	inbound := packet.Info{
		Inbound:  true,
		Version:  outbound.Version,
		Protocol: outbound.Protocol,
		Src:      outbound.Dst,
		SrcPort:  outbound.DstPort,
		Dst:      outbound.Src,
		DstPort:  outbound.SrcPort,
		PID:      process.UndefinedProcessID,
		SeenAt:   time.Now(),
	}
	realPkt := newTestRealPacket(inbound)

	conn := NewIncompleteConnection(realPkt)
	defer conns.delete(conn)

	if !conn.Inbound {
		t.Error("expected the direction of the intercepted packet to be kept")
	}
	if conn.PID != 4249 {
		t.Errorf("expected pid 4249 from hint, got %d", conn.PID)
	}
}

func TestInfoAndRealPacketShareConnectionID(t *testing.T) {
	t.Parallel()

	for _, inbound := range []bool{false, true} {
		for _, protocol := range []packet.IPProtocol{packet.TCP, packet.UDP} {
			info := testPacketInfo(10500, 443, 4248, inbound)
			info.Protocol = protocol

			infoPkt := packet.NewInfoPacket(info)
			realPkt := newTestRealPacket(info)

			if infoPkt.GetConnectionID() != realPkt.GetConnectionID() {
				t.Errorf(
					"connection ID mismatch for protocol=%s inbound=%v: %q != %q",
					protocol, inbound, infoPkt.GetConnectionID(), realPkt.GetConnectionID(),
				)
			}
		}
	}
}
