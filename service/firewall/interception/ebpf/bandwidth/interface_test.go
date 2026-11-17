//go:build linux

package ebpf

import (
	"context"
	"testing"

	"github.com/cilium/ebpf"

	"github.com/safing/portmaster/service/network/packet"
)

// testBandwidthSource is a bandwidthSource backed by a Go map.
type testBandwidthSource struct {
	entries map[bpfSkKey]bpfSkInfo
	// hidden entries are skipped by forEach, as may happen for entries of an
	// eBPF map that is changed while it is iterated.
	hidden     map[bpfSkKey]struct{}
	iterateErr error
}

func (s *testBandwidthSource) forEach(fn func(key bpfSkKey, info bpfSkInfo) bool) error {
	for key, info := range s.entries {
		if _, ok := s.hidden[key]; ok {
			continue
		}
		if !fn(key, info) {
			return nil
		}
	}
	return s.iterateErr
}

func (s *testBandwidthSource) lookup(key bpfSkKey) (bpfSkInfo, bool, error) {
	info, ok := s.entries[key]
	return info, ok, nil
}

func testSkKey(srcPort uint16) bpfSkKey {
	return bpfSkKey{
		SrcIp:    [4]uint32{0x0100007f},
		DstIp:    [4]uint32{0x08080808},
		SrcPort:  srcPort,
		DstPort:  53,
		Protocol: 17,
	}
}

// collectBandwidth runs one report and returns the reported bytes per
// connection ID.
func collectBandwidth(t *testing.T, r *bandwidthReporter, source bandwidthSource, queueSize int) map[string][2]uint64 {
	t.Helper()

	updates := make(chan *packet.BandwidthUpdate, queueSize)
	r.report(context.Background(), source, updates)
	close(updates)

	result := make(map[string][2]uint64)
	for update := range updates {
		if update.Method != packet.Additive {
			t.Errorf("update %s is not additive", update)
		}
		if _, ok := result[update.ConnID]; ok {
			t.Errorf("connection %s was reported twice in one report", update.ConnID)
		}
		result[update.ConnID] = [2]uint64{update.BytesReceived, update.BytesSent}
	}
	return result
}

func connIDOf(key bpfSkKey) string {
	return packet.CreateConnectionID(
		packet.IPProtocol(key.Protocol),
		convertArrayToIP(key.SrcIp, false), key.SrcPort,
		convertArrayToIP(key.DstIp, false), key.DstPort,
		false,
	)
}

func expectBandwidth(t *testing.T, got map[string][2]uint64, want map[bpfSkKey][2]uint64) {
	t.Helper()

	if len(got) != len(want) {
		t.Errorf("got %d updates, expected %d: %v", len(got), len(want), got)
	}
	for key, bytes := range want {
		if got[connIDOf(key)] != bytes {
			t.Errorf("connection %s: got rx/tx %v, expected %v", connIDOf(key), got[connIDOf(key)], bytes)
		}
	}
}

func TestBandwidthReporterDeltas(t *testing.T) {
	t.Parallel()

	a, b := testSkKey(1000), testSkKey(2000)
	source := &testBandwidthSource{entries: map[bpfSkKey]bpfSkInfo{
		a: {Rx: 100, Tx: 10},
		b: {Rx: 0, Tx: 0},
	}}
	r := newBandwidthReporter()

	// The first report holds everything counted so far.
	expectBandwidth(t, collectBandwidth(t, r, source, 10), map[bpfSkKey][2]uint64{a: {100, 10}})

	// Nothing changed: nothing to report. The entry in the source is not
	// marked or changed in any way.
	expectBandwidth(t, collectBandwidth(t, r, source, 10), nil)
	if source.entries[a] != (bpfSkInfo{Rx: 100, Tx: 10}) {
		t.Errorf("source entry was changed: %+v", source.entries[a])
	}

	// Only the added bytes are reported. This is what a successor connection
	// with the same 5-tuple gets, instead of all bytes of its predecessors.
	source.entries[a] = bpfSkInfo{Rx: 150, Tx: 10}
	source.entries[b] = bpfSkInfo{Rx: 0, Tx: 7}
	expectBandwidth(t, collectBandwidth(t, r, source, 10), map[bpfSkKey][2]uint64{a: {50, 0}, b: {0, 7}})

	// A counter that went backwards was reset and holds the bytes since.
	source.entries[a] = bpfSkInfo{Rx: 20, Tx: 15}
	expectBandwidth(t, collectBandwidth(t, r, source, 10), map[bpfSkKey][2]uint64{a: {20, 5}})
	source.entries[a] = bpfSkInfo{Rx: 25, Tx: 15}
	expectBandwidth(t, collectBandwidth(t, r, source, 10), map[bpfSkKey][2]uint64{a: {5, 0}})
}

func TestBandwidthReporterFullQueue(t *testing.T) {
	t.Parallel()

	source := &testBandwidthSource{entries: make(map[bpfSkKey]bpfSkInfo)}
	want := make(map[bpfSkKey][2]uint64)
	for port := uint16(1); port <= 10; port++ {
		source.entries[testSkKey(port)] = bpfSkInfo{Rx: uint64(port), Tx: 1}
		want[testSkKey(port)] = [2]uint64{uint64(port), 1}
	}
	r := newBandwidthReporter()

	// Report with a queue that only fits some updates at a time: Every byte
	// must be reported exactly once in the end.
	total := make(map[string][2]uint64)
	for range 4 {
		for connID, bytes := range collectBandwidth(t, r, source, 3) {
			sum := total[connID]
			total[connID] = [2]uint64{sum[0] + bytes[0], sum[1] + bytes[1]}
		}
	}
	expectBandwidth(t, total, want)
	expectBandwidth(t, collectBandwidth(t, r, source, 3), nil)
}

func TestBandwidthReporterForgetsGoneEntries(t *testing.T) {
	t.Parallel()

	a, b := testSkKey(1000), testSkKey(2000)
	source := &testBandwidthSource{entries: map[bpfSkKey]bpfSkInfo{
		a: {Rx: 100, Tx: 10},
		b: {Rx: 200, Tx: 20},
	}}
	r := newBandwidthReporter()
	collectBandwidth(t, r, source, 10)

	// An entry that the iteration missed, but that still exists, is not
	// forgotten and its new bytes are reported.
	source.hidden = map[bpfSkKey]struct{}{a: {}}
	source.entries[a] = bpfSkInfo{Rx: 101, Tx: 10}
	expectBandwidth(t, collectBandwidth(t, r, source, 10), map[bpfSkKey][2]uint64{a: {1, 0}})
	expectBandwidth(t, collectBandwidth(t, r, source, 10), nil)
	source.hidden = nil
	expectBandwidth(t, collectBandwidth(t, r, source, 10), nil)

	// Nothing is forgotten when the iteration failed.
	source.hidden = map[bpfSkKey]struct{}{a: {}, b: {}}
	source.iterateErr = ebpf.ErrIterationAborted
	expectBandwidth(t, collectBandwidth(t, r, source, 10), nil)
	if len(r.reported) != 2 {
		t.Errorf("%d entries are remembered after a failed iteration, expected 2", len(r.reported))
	}
	source.hidden, source.iterateErr = nil, nil

	// An entry that was evicted from the map is forgotten.
	delete(source.entries, a)
	expectBandwidth(t, collectBandwidth(t, r, source, 10), nil)
	if len(r.reported) != 1 {
		t.Errorf("%d entries are remembered, expected 1", len(r.reported))
	}

	// When it shows up again, it is a new entry.
	source.entries[a] = bpfSkInfo{Rx: 500, Tx: 0}
	expectBandwidth(t, collectBandwidth(t, r, source, 10), map[bpfSkKey][2]uint64{a: {500, 0}})
}
