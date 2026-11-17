package crew

import (
	"errors"
	"testing"

	"github.com/safing/portmaster/spn/peerdsd"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
)

// TestPingOpRejectsOversizedMsgPack checks that a msgpack ping request that
// declares more data than it holds is rejected before decoding. The msgpack
// decoder allocates the declared length up front, so 12 bytes sent by any
// client could make a Hub allocate 4GB.
// The length used here is smaller to keep the test cheap without the fix.
func TestPingOpRejectsOversizedMsgPack(t *testing.T) {
	t.Parallel()

	a, _, err := terminal.NewSimpleTestTerminalPair(0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	// PingOpRequest.Nonce with a declared length of 64MB.
	data := []byte{'M', 0x81, 0xa5, 'N', 'o', 'n', 'c', 'e', 0xc6, 0x04, 0x00, 0x00, 0x00}
	_, tErr := startPingOp(a, 1, container.New(data))
	if !errors.Is(tErr, peerdsd.ErrLengthExceedsData) {
		t.Errorf("expected ErrLengthExceedsData, got %v", tErr)
	}
}
