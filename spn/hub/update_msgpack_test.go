package hub

import (
	"errors"
	"testing"
	"time"

	"github.com/safing/portmaster/spn/peerdsd"
)

// TestHubMsgsRejectOversizedMsgPack checks that msgpack hub messages that
// declare more data than they hold are rejected before decoding. The msgpack
// decoder allocates the declared length up front, so 12 bytes could make it
// allocate 4GB (byte string) or 32GB (slice of pointers) and crash the process.
// The lengths used here are smaller to keep the test cheap without the fix.
func TestHubMsgsRejectOversizedMsgPack(t *testing.T) {
	t.Parallel()

	// Letter.Data with a declared length of 64MB.
	data := []byte{'M', 0x81, 0xa4, 'D', 'a', 't', 'a', 0xc6, 0x04, 0x00, 0x00, 0x00}
	_, _, _, err := OpenHubMsg(nil, data, "test-msgpack-letter", true) //nolint:dogsled
	if !errors.Is(err, peerdsd.ErrLengthExceedsData) {
		t.Errorf("letter: expected ErrLengthExceedsData, got %v", err)
	}

	// Status.Lanes with a declared length of 1M in a correctly signed letter.
	env, hubID := newTestHubEnvelope(t)
	a := &Announcement{
		ID:         hubID,
		Timestamp:  time.Now().Unix(),
		Name:       "msgpack",
		Transports: []string{"tcp:1718"},
	}
	data, err = a.Export(env)
	if err != nil {
		t.Fatal(err)
	}
	h, _, _, err := ApplyAnnouncement(nil, data, "test-msgpack-status", ScopePublic, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err = SignHubMsg(
		[]byte{'M', 0x81, 0xa5, 'L', 'a', 'n', 'e', 's', 0xdd, 0x00, 0x10, 0x00, 0x00},
		env, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = ApplyStatus(h, data, "test-msgpack-status", ScopePublic, false) //nolint:dogsled
	if !errors.Is(err, peerdsd.ErrLengthExceedsData) {
		t.Errorf("status: expected ErrLengthExceedsData, got %v", err)
	}
}
