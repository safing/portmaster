package token

import (
	"crypto/elliptic"
	"errors"
	"testing"
)

func newTestPBlindVerifier(tb testing.TB) *PBlindHandler {
	tb.Helper()

	verifier, err := NewPBlindHandler(PBlindOptions{
		Zone:       PBlindTestZone,
		Curve:      elliptic.P256(),
		PublicKey:  "285oMDh3w5mxyFgpmmURifKfhkcqwwsdnePpPZ6Nqm8cc",
		UseSerials: true,
		BatchSize:  1000,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return verifier
}

// TestPBlindVerifyWithoutSignature checks that a token without a signature is
// rejected instead of causing a panic.
func TestPBlindVerifyWithoutSignature(t *testing.T) {
	t.Parallel()

	verifier := newTestPBlindVerifier(t)

	data, err := (&PBlindToken{Serial: 1, Token: []byte("token")}).Pack()
	if err != nil {
		t.Fatal(err)
	}
	err = verifier.Verify(&Token{Zone: PBlindTestZone, Data: data})
	if !errors.Is(err, ErrTokenMalformed) {
		t.Fatalf("expected malformed token error, got %v", err)
	}
}

func FuzzPBlindVerify(f *testing.F) {
	data, err := (&PBlindToken{Serial: 1, Token: []byte("token")}).Pack()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte{})

	verifier := newTestPBlindVerifier(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		err := verifier.Verify(&Token{Zone: PBlindTestZone, Data: data})
		if err == nil {
			t.Fatal("fuzzed token verified")
		}
		_, _ = ParseRawToken(data)
		_, _ = ParseToken(string(data))
	})
}
