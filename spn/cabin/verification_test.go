package cabin

import (
	"fmt"
	"testing"
)

func TestVerification(t *testing.T) {
	t.Parallel()

	id, err := CreateIdentity(module.m.Ctx(), "test")
	if err != nil {
		t.Fatal(err)
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"a", "b", "c",
		"", "", "", nil,
	); err != nil {
		t.Fatal(err)
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"x", "b", "c",
		"", "", "", nil,
	); err == nil {
		t.Fatal("should fail on purpose mismatch")
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"a", "x", "c",
		"", "", "", nil,
	); err == nil {
		t.Fatal("should fail on client ref mismatch")
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"a", "b", "x",
		"", "", "", nil,
	); err == nil {
		t.Fatal("should fail on server ref mismatch")
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"a", "b", "c",
		"x", "", "", nil,
	); err == nil {
		t.Fatal("should fail on purpose mismatch")
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"a", "b", "c",
		"", "x", "", nil,
	); err == nil {
		t.Fatal("should fail on client ref mismatch")
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"a", "b", "c",
		"", "", "x", nil,
	); err == nil {
		t.Fatal("should fail on server ref mismatch")
	}

	if err := testVerificationWith(
		t, id,
		"a", "b", "c",
		"a", "b", "c",
		"", "", "", []byte{1, 2, 3, 4},
	); err == nil {
		t.Fatal("should fail on challenge mismatch")
	}
}

func testVerificationWith(
	t *testing.T, id *Identity,
	purpose1, clientRef1, serverRef1 string, //nolint:unparam
	purpose2, clientRef2, serverRef2 string,
	mitmPurpose, mitmClientRef, mitmServerRef string,
	mitmChallenge []byte,
) error {
	t.Helper()

	v, request, err := CreateVerificationRequest(purpose1, clientRef1, serverRef1)
	if err != nil {
		return fmt.Errorf("failed to create verification request: %w", err)
	}

	response, err := id.SignVerificationRequest(request, purpose2, clientRef2, serverRef2)
	if err != nil {
		return fmt.Errorf("failed to sign verification response: %w", err)
	}

	if mitmPurpose != "" {
		v.Purpose = mitmPurpose
	}
	if mitmClientRef != "" {
		v.ClientReference = mitmClientRef
	}
	if mitmServerRef != "" {
		v.ServerReference = mitmServerRef
	}
	if mitmChallenge != nil {
		v.Challenge = mitmChallenge
	}

	err = v.Verify(response, id.Hub)
	if err != nil {
		return fmt.Errorf("failed to verify: %w", err)
	}

	return nil
}
