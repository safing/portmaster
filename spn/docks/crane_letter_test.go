package docks

import (
	"errors"
	"testing"

	"github.com/safing/jess"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// TestCraneDecryptLetterWithHugeBlockSize checks that a crane does not
// allocate the block size of a received letter, which is parsed before the
// letter is authenticated. With jess.LetterFromWire, this test crashed with
// "fatal error: runtime: out of memory".
func TestCraneDecryptLetterWithHugeBlockSize(t *testing.T) {
	t.Parallel()

	// The session is not used, as parsing the letter fails.
	crane := &Crane{jession: &jess.Session{}}
	// Data message with a nonce of 4TB.
	data := append([]byte{1, 0}, varint.Pack64(1<<42)...)
	_, err := crane.decrypt(container.New(data))
	if !errors.Is(err, terminal.ErrBlockTooBig) {
		t.Errorf("expected ErrBlockTooBig, got %v", err)
	}
}
