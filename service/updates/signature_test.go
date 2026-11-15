package updates

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/safing/jess"
)

// newSigningKey generates a key and a trust store that holds its public half.
func newSigningKey(t *testing.T, name string) (signet *jess.Signet, trustStore *jess.MemTrustStore) {
	t.Helper()

	signet, err := jess.GenerateSignet("Ed25519", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := signet.AssignUUID(); err != nil {
		t.Fatal(err)
	}
	signet.Info = &jess.SignetInfo{Name: name}
	// Serialize the key into the signet; an envelope treats a signet
	// without serialized key as a reference into a trust store.
	if err := signet.StoreKey(); err != nil {
		t.Fatal(err)
	}

	rcpt, err := signet.AsRecipient()
	if err != nil {
		t.Fatal(err)
	}
	trustStore = jess.NewMemTrustStore()
	if err := trustStore.StoreSignet(rcpt); err != nil {
		t.Fatal(err)
	}
	return signet, trustStore
}

// TestParseIndexVerifiesSignature checks that ParseIndex, given a trust
// store, accepts exactly the index signed with a trusted key: a signature
// from another key, no signature, and a change after signing all fail.
func TestParseIndexVerifiesSignature(t *testing.T) {
	t.Parallel()

	key, trustStore := newSigningKey(t, "test key")
	_, otherTrustStore := newSigningKey(t, "other key")

	dir := t.TempDir()
	if err := GenerateMockFolder(dir, "Test", "1.0.0", time.Now()); err != nil {
		t.Fatal(err)
	}
	index, err := LoadIndex(filepath.Join(dir, "index.json"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := index.Export(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := index.Export(key, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ParseIndex(signed, "", trustStore); err != nil {
		t.Errorf("signed index must verify: %s", err)
	}
	if _, err := ParseIndex(signed, "", otherTrustStore); err == nil {
		t.Error("index signed with an unknown key must fail")
	}
	if _, err := ParseIndex(unsigned, "", trustStore); err == nil {
		t.Error("unsigned index must fail")
	}
	tampered := bytes.Replace(signed, []byte(`"Test"`), []byte(`"Tampered"`), 1)
	if bytes.Equal(tampered, signed) {
		t.Fatal("test did not tamper with the index")
	}
	if _, err := ParseIndex(tampered, "", trustStore); err == nil {
		t.Error("tampered index must fail")
	}
}
