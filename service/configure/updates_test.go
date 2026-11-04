package configure

import (
	"testing"

	"github.com/safing/jess"
)

// TestIntelSigningKeysAreSeparate makes sure that the intel trust store holds
// every intel key and none of the code signing keys.
func TestIntelSigningKeysAreSeparate(t *testing.T) {
	t.Parallel()

	if len(IntelSigningKeys) == 0 {
		t.Fatal("no intel signing keys")
	}
	for _, key := range IntelSigningKeys {
		rcpt, err := jess.RecipientFromTextFormat(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := IntelSigningTrustStore.GetSignet(rcpt.ID, true); err != nil {
			t.Errorf("intel key %s missing from the intel trust store: %s", rcpt.ID, err)
		}
	}
	for _, key := range BinarySigningKeys {
		rcpt, err := jess.RecipientFromTextFormat(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := IntelSigningTrustStore.GetSignet(rcpt.ID, true); err == nil {
			t.Errorf("code signing key %s must not be in the intel trust store", rcpt.ID)
		}
	}
}
