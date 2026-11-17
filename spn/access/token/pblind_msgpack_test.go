package token

import (
	"errors"
	"testing"

	"github.com/safing/portmaster/spn/peerdsd"
)

// TestUnpackPBlindTokenRejectsOversizedMsgPack checks that a msgpack token
// that declares more data than it holds is rejected before decoding. The
// msgpack decoder allocates the declared length up front, so 12 bytes sent by
// any client before authentication could make a Hub allocate 4GB.
// The length used here is smaller to keep the test cheap without the fix.
func TestUnpackPBlindTokenRejectsOversizedMsgPack(t *testing.T) {
	t.Parallel()

	// PBlindToken.Token with a declared length of 64MB.
	data := []byte{'M', 0x81, 0xa5, 'T', 'o', 'k', 'e', 'n', 0xc6, 0x04, 0x00, 0x00, 0x00}
	_, err := UnpackPBlindToken(data)
	if !errors.Is(err, peerdsd.ErrLengthExceedsData) {
		t.Errorf("expected ErrLengthExceedsData, got %v", err)
	}
}
