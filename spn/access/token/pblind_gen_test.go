package token

import (
	"crypto/elliptic"
	"fmt"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/rot256/pblind"
)

func TestGeneratePBlindKeys(t *testing.T) {
	t.Parallel()

	for _, curve := range []elliptic.Curve{
		elliptic.P256(),
		elliptic.P384(),
		elliptic.P521(),
	} {
		privateKey, err := pblind.NewSecretKey(curve)
		if err != nil {
			t.Fatal(err)
		}
		publicKey := privateKey.GetPublicKey()

		fmt.Printf(
			"%s (%dbit) private key: %s\n",
			curve.Params().Name,
			curve.Params().BitSize,
			base58.Encode(privateKey.Bytes()),
		)
		fmt.Printf(
			"%s (%dbit) public key: %s\n",
			curve.Params().Name,
			curve.Params().BitSize,
			base58.Encode(publicKey.Bytes()),
		)
	}
}
