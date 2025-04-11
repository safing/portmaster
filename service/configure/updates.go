package configure

import (
	"github.com/safing/jess"
)

var (
	DefaultBinaryIndexName = "Portmaster Binaries"
	DefaultIntelIndexName  = "intel"

	DefaultStableBinaryIndexURLs = []string{
		"https://updates.safing.io/stable.v3.json",
	}
	DefaultSpnStableBinaryIndexURLs = []string{
		"https://updates.safing.io/stable.v3.json",
	}
	DefaultBetaBinaryIndexURLs = []string{
		"https://updates.safing.io/beta.v3.json",
	}
	DefaultStagingBinaryIndexURLs = []string{
		"https://updates.safing.io/staging.v3.json",
	}
	DefaultSupportBinaryIndexURLs = []string{
		"https://updates.safing.io/support.v3.json",
	}

	DefaultIntelIndexURLs = []string{
		"https://updates.safing.io/intel.v3.json",
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

// GetBinaryUpdateURLs returns the correct binary update URLs for the given release channel.
// Silently falls back to stable if release channel is invalid.
func GetBinaryUpdateURLs(releaseChannel string) []string {
	switch releaseChannel {
	case "stable":
		return DefaultStableBinaryIndexURLs
	case "beta":
		return DefaultBetaBinaryIndexURLs
	case "staging":
		return DefaultStagingBinaryIndexURLs
	case "support":
		return DefaultSupportBinaryIndexURLs
	default:
		return DefaultStableBinaryIndexURLs
	}
}
