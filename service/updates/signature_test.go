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
	err = signet.AssignUUID()
	if err != nil {
		t.Fatal(err)
	}
	signet.Info = &jess.SignetInfo{Name: name}
	// Serialize the key into the signet; an envelope treats a signet
	// without serialized key as a reference into a trust store.
	err = signet.StoreKey()
	if err != nil {
		t.Fatal(err)
	}

	rcpt, err := signet.AsRecipient()
	if err != nil {
		t.Fatal(err)
	}
	trustStore = jess.NewMemTrustStore()
	err = trustStore.StoreSignet(rcpt)
	if err != nil {
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
	err := GenerateMockFolder(dir, "Test", "1.0.0", time.Now())
	if err != nil {
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

	_, err = ParseIndex(signed, "", trustStore)
	if err != nil {
		t.Errorf("signed index must verify: %s", err)
	}
	_, err = ParseIndex(signed, "", otherTrustStore)
	if err == nil {
		t.Error("index signed with an unknown key must fail")
	}
	_, err = ParseIndex(unsigned, "", trustStore)
	if err == nil {
		t.Error("unsigned index must fail")
	}
	tampered := bytes.Replace(signed, []byte(`"Test"`), []byte(`"Tampered"`), 1)
	if bytes.Equal(tampered, signed) {
		t.Fatal("test did not tamper with the index")
	}
	_, err = ParseIndex(tampered, "", trustStore)
	if err == nil {
		t.Error("tampered index must fail")
	}
}
