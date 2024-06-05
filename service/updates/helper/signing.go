package helper

import (
	"github.com/safing/jess"
	"github.com/safing/portmaster/base/updater"
)

var (
	// VerificationConfig holds the complete verification configuration for the registry.
	VerificationConfig = map[string]*updater.VerificationOptions{
		"": { // Default.
			TrustStore:     BinarySigningTrustStore,
			DownloadPolicy: updater.SignaturePolicyRequire,
			DiskLoadPolicy: updater.SignaturePolicyWarn,
		},
		"all/intel/": nil, // Disable until IntelHub supports signing.
	}

	// BinarySigningKeys holds the signing keys in text format.
	BinarySigningKeys = []string{
		// Safing Code Signing Key #1
		"recipient:public-ed25519-key:safing-code-signing-key-1:92bgBLneQUWrhYLPpBDjqHbpFPuNVCPAaivQ951A4aq72HcTiw7R1QmPJwFM1mdePAvEVDjkeb8S4fp2pmRCsRa8HrCvWQEjd88rfZ6TznJMfY4g7P8ioGFjfpyx2ZJ8WCZJG5Qt4Z9nkabhxo2Nbi3iywBTYDLSbP5CXqi7jryW7BufWWuaRVufFFzhwUC2ryWFWMdkUmsAZcvXwde4KLN9FrkWAy61fGaJ8GCwGnGCSitANnU2cQrsGBXZzxmzxwrYD",
		// Safing Code Signing Key #2
		"recipient:public-ed25519-key:safing-code-signing-key-2:92bgBLneQUWrhYLPpBDjqHbPC2d1o5JMyZFdavWBNVtdvbPfzDewLW95ScXfYPHd3QvWHSWCtB4xpthaYWxSkK1kYiGp68DPa2HaU8yQ5dZhaAUuV4Kzv42pJcWkCeVnBYqgGBXobuz52rFqhDJy3rz7soXEmYhJEJWwLwMeioK3VzN3QmGSYXXjosHMMNC76rjufSoLNtUQUWZDSnHmqbuxbKMCCsjFXUGGhtZVyb7bnu7QLTLk6SKHBJDMB6zdL9sw3",
	}

	// BinarySigningTrustStore is an in-memory trust store with the signing keys.
	BinarySigningTrustStore = jess.NewMemTrustStore()
)

func init() {
	for _, signingKey := range BinarySigningKeys {
		rcpt, err := jess.RecipientFromTextFormat(signingKey)
		if err != nil {
			panic(err)
		}
		err = BinarySigningTrustStore.StoreSignet(rcpt)
		if err != nil {
			panic(err)
		}
	}
}
