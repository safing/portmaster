package cabin

import (
	"errors"
	"testing"

	"github.com/safing/portmaster/spn/peerdsd"
)

// TestVerificationRejectsOversizedMsgPack checks that msgpack verification
// requests that declare more data than they hold are rejected before decoding.
// The msgpack decoder allocates the declared length up front, so 12 bytes sent
// by any client could make a Hub allocate 4GB.
// The length used here is smaller to keep the test cheap without the fix.
func TestVerificationRejectsOversizedMsgPack(t *testing.T) {
	t.Parallel()

	id, err := CreateIdentity(module.m.Ctx(), "test")
	if err != nil {
		t.Fatal(err)
	}

	// Verification.Challenge with a declared length of 64MB.
	request := []byte{'M', 0x81, 0xa9, 'C', 'h', 'a', 'l', 'l', 'e', 'n', 'g', 'e', 0xc6, 0x04, 0x00, 0x00, 0x00}
	_, err = id.SignVerificationRequest(request, "a", "b", "c")
	if !errors.Is(err, peerdsd.ErrLengthExceedsData) {
		t.Errorf("expected ErrLengthExceedsData, got %v", err)
	}

	// Response letter with Letter.Data with a declared length of 64MB.
	v, _, err := CreateVerificationRequest("a", "b", "c")
	if err != nil {
		t.Fatal(err)
	}
	response := []byte{'M', 0x81, 0xa4, 'D', 'a', 't', 'a', 0xc6, 0x04, 0x00, 0x00, 0x00}
	err = v.Verify(response, id.Hub)
	if !errors.Is(err, peerdsd.ErrLengthExceedsData) {
		t.Errorf("expected ErrLengthExceedsData, got %v", err)
	}
}
