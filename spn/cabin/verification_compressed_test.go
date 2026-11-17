package cabin

import (
	"errors"
	"testing"

	"github.com/safing/jess"
	"github.com/safing/structures/dsd"
)

// TestVerificationRejectsCompressed checks that gzip compressed verification
// requests and responses are rejected before decompression. dsd.Load
// decompresses them into an unbounded buffer, so a few KB sent by any client
// (requests) or any Hub (responses) could make the receiver allocate many MB.
func TestVerificationRejectsCompressed(t *testing.T) {
	t.Parallel()

	id, err := CreateIdentity(module.m.Ctx(), "test")
	if err != nil {
		t.Fatal(err)
	}

	// Compressed request.
	v, request, err := CreateVerificationRequest("a", "b", "c")
	if err != nil {
		t.Fatal(err)
	}
	compressedRequest, err := dsd.DumpAndCompress(v, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	_, err = id.SignVerificationRequest(compressedRequest, "a", "b", "c")
	if !errors.Is(err, dsd.ErrIncompatibleFormat) {
		t.Errorf("compressed request: expected ErrIncompatibleFormat, got %v", err)
	}

	// Compressed response letter.
	response, err := id.SignVerificationRequest(request, "a", "b", "c")
	if err != nil {
		t.Fatal(err)
	}
	err = v.Verify(response, id.Hub)
	if err != nil {
		t.Fatalf("valid response: %s", err)
	}
	letter, err := jess.LetterFromDSD(response)
	if err != nil {
		t.Fatal(err)
	}
	compressedResponse, err := dsd.DumpAndCompress(letter, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	err = v.Verify(compressedResponse, id.Hub)
	if !errors.Is(err, dsd.ErrIncompatibleFormat) {
		t.Errorf("compressed response letter: expected ErrIncompatibleFormat, got %v", err)
	}

	// Compressed response data in a correctly signed letter.
	compressedData, err := dsd.DumpAndCompress(v, dsd.JSON, dsd.GZIP)
	if err != nil {
		t.Fatal(err)
	}
	e := jess.NewUnconfiguredEnvelope()
	e.SuiteID = verificationSigningSuite
	e.Senders = []*jess.Signet{id.Signet}
	jession, err := e.Correspondence(nil)
	if err != nil {
		t.Fatal(err)
	}
	letter, err = jession.Close(compressedData)
	if err != nil {
		t.Fatal(err)
	}
	signedResponse, err := letter.ToDSD(dsd.JSON)
	if err != nil {
		t.Fatal(err)
	}
	err = v.Verify(signedResponse, id.Hub)
	if !errors.Is(err, dsd.ErrIncompatibleFormat) {
		t.Errorf("compressed response data: expected ErrIncompatibleFormat, got %v", err)
	}
}
