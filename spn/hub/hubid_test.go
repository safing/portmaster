package hub

import (
	"errors"
	"strings"
	"testing"

	"github.com/mr-tron/base58"

	"github.com/safing/jess"
	"github.com/safing/jess/lhash"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

// hugeLengthHubID is a short base58 string that decodes to a labeled hash
// claiming a digest of about 2.9 TB. Found by FuzzParseTransport.
const hugeLengthHubID = "wns3eWStj"

// TestHubIDWithHugeDigestLength checks that Hub IDs that announce a huge
// digest length are rejected without allocating that length.
func TestHubIDWithHugeDigestLength(t *testing.T) {
	t.Parallel()

	// Hub ID check, as done in OpenHubMsg.
	if verifyHubID(hugeLengthHubID, "Ed25519", []byte{1}) {
		t.Fatal("invalid hub ID verified")
	}
	maxLength := base58.Encode(append(varint.Pack64(16), varint.Pack64(1<<63)...))
	if verifyHubID(maxLength, "Ed25519", []byte{1}) {
		t.Fatal("invalid hub ID verified")
	}

	// Bootstrap hub parsing.
	_, _, _, err := ParseBootstrapHub("tcp://1.2.3.4:17#" + hugeLengthHubID) //nolint:dogsled
	if err == nil {
		t.Fatal("invalid bootstrap hub ID accepted")
	}

	// Full hub msg with the ID set as signer, as received from the network.
	letter := &jess.Letter{
		Version:    1,
		SuiteID:    jess.SuiteSignV1,
		Data:       []byte("{}"),
		Keys:       []*jess.Seal{{Value: []byte{1}}},
		Signatures: []*jess.Seal{{ID: hugeLengthHubID, Scheme: "Ed25519", Value: []byte{1}}},
	}
	data, err := letter.ToDSD(dsd.JSON)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = OpenHubMsg(nil, data, "test-huge-id", true) //nolint:dogsled
	if err == nil {
		t.Fatal("hub msg with invalid hub ID accepted")
	}

	// Valid IDs must still work.
	_, public, err := CreateHubSignet("Ed25519", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyHubID(public.ID, public.Scheme, public.Key) {
		t.Fatal("valid hub ID not verified")
	}
	_, id, _, err := ParseBootstrapHub("tcp://1.2.3.4:17#" + public.ID)
	if err != nil || id != public.ID {
		t.Fatalf("valid bootstrap hub rejected: %s", err)
	}
}

// TestLabeledHashFromBase58LimitsLength checks that overlong IDs are rejected
// before they are decoded, as decoding base58 takes quadratic time.
func TestLabeledHashFromBase58LimitsLength(t *testing.T) {
	t.Parallel()

	_, err := labeledHashFromBase58(strings.Repeat("z", maxEncodedLabeledHashLength+1))
	if !errors.Is(err, errLabeledHashTooLong) {
		t.Errorf("expected errLabeledHashTooLong, got %v", err)
	}
	if verifyHubID(strings.Repeat("z", 16000), "Ed25519", []byte{1}) {
		t.Error("overlong hub ID verified")
	}

	// The biggest digests are within the limit.
	for _, alg := range []lhash.Algorithm{lhash.SHA2_512, lhash.SHA3_512, lhash.BLAKE2b_512, lhash.BLAKE2b_256} {
		encoded := lhash.Digest(alg, []byte("data")).Base58()
		if len(encoded) > 91 {
			t.Errorf("unexpected encoded length %d", len(encoded))
		}
		_, err := labeledHashFromBase58(encoded)
		if err != nil {
			t.Errorf("failed to load %s: %s", alg, err)
		}
	}
}

func BenchmarkVerifyHubIDOverlong(b *testing.B) {
	id := strings.Repeat("z", 16000)
	for range b.N {
		verifyHubID(id, "Ed25519", make([]byte, 32))
	}
}
