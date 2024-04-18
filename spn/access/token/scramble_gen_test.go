package token

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/mr-tron/base58"

	"github.com/safing/jess/lhash"
)

type genAlgs struct {
	alg  lhash.Algorithm
	name string
}

func TestGenerateScrambleKeys(t *testing.T) {
	t.Parallel()

	for _, alg := range []genAlgs{
		{alg: lhash.SHA2_256, name: "SHA2_256"},
		{alg: lhash.SHA3_256, name: "SHA3_256"},
		{alg: lhash.SHA3_512, name: "SHA3_512"},
		{alg: lhash.BLAKE2b_256, name: "BLAKE2b_256"},
	} {
		token := make([]byte, scrambleSecretSize)
		n, err := rand.Read(token)
		if err != nil {
			t.Fatal(err)
		}
		if n != scrambleSecretSize {
			t.Fatalf("only got %d bytes", n)
		}
		scrambledToken := lhash.Digest(alg.alg, token).Bytes()

		fmt.Printf(
			"%s secret token: %s\n",
			alg.name,
			base58.Encode(token),
		)
		fmt.Printf(
			"%s scrambled (public) token: %s\n",
			alg.name,
			base58.Encode(scrambledToken),
		)
	}
}
