package token

import (
	"bytes"
	"errors"
	"testing"

	"github.com/safing/structures/dsd"
)

// TestUnpackPBlindTokenRejectsCompressed checks that a gzip compressed token
// is rejected before decompression. Tokens are received from any client before
// authentication, and dsd.Load decompresses them into an unbounded buffer, so a
// few KB could make a Hub allocate many MB.
func TestUnpackPBlindTokenRejectsCompressed(t *testing.T) {
	t.Parallel()

	pbt := &PBlindToken{Serial: 1, Token: []byte("token")}

	// Packed tokens are accepted.
	data, err := pbt.Pack()
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := UnpackPBlindToken(data)
	if err != nil {
		t.Fatal(err)
	}
	if unpacked.Serial != 1 || !bytes.Equal(unpacked.Token, pbt.Token) {
		t.Fatalf("unexpected token: %+v", unpacked)
	}

	// Compressed tokens are rejected.
	data, err = dsd.DumpAndCompress(pbt, dsd.CBOR, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	_, err = UnpackPBlindToken(data)
	if !errors.Is(err, dsd.ErrIncompatibleFormat) {
		t.Errorf("expected ErrIncompatibleFormat, got %v", err)
	}
}
