package terminal

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// TestFlowQueueSpaceReportLimit checks that a space report always fits the 16
// bit number the receiving side parses, without losing any space.
func TestFlowQueueSpaceReportLimit(t *testing.T) {
	t.Parallel()

	const queueSize = 200_000 // Allowed up to MaxQueueSize.

	dfq := NewDuplexFlowQueue(context.Background(), queueSize, func(*Msg, time.Duration) {})
	// Pretend all of the initially reported space was used up by the other end.
	atomic.StoreInt32(dfq.reportedSpace, 0)

	var total int64
	for range 10 {
		report := dfq.reportableRecvSpace()
		if report == 0 {
			break
		}
		if report > math.MaxUint16 {
			t.Fatalf("reported space %d does not fit into 16 bits", report)
		}

		// The receiving side must be able to parse the report.
		c := container.New(varint.Pack64(uint64(report))) //nolint:gosec // Checked to be positive above.
		parsed, err := c.GetNextN16()
		if err != nil {
			t.Fatalf("failed to parse report of %d: %s", report, err)
		}
		if int32(parsed) != report {
			t.Fatalf("parsed %d, reported %d", parsed, report)
		}

		total += int64(report)
	}

	// No space must be lost.
	if total != queueSize {
		t.Fatalf("reported %d in total, expected %d", total, queueSize)
	}
	if got := atomic.LoadInt32(dfq.reportedSpace); got != queueSize {
		t.Fatalf("reportedSpace is %d, expected %d", got, queueSize)
	}
}
