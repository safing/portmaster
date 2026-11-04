package configure

import (
	"github.com/safing/jess"
)

var (
	DefaultBinaryIndexName = "Portmaster Binaries"
	DefaultIntelIndexName  = "Portmaster Intel"

	DefaultStableBinaryIndexURLs = []string{
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

	// IntelSigningKeys holds the intel index signing keys in text format.
	// They must never be the code signing keys above: an intel key may only
	// be able to forge intel data, never binaries.
	IntelSigningKeys = []string{
		// Safing Intel Signing Key #1
		"recipient:public-ed25519-key:safing-intel-signing-key-1:cS43veLU6cSnMwJbhE7E47UZwxfNTF8wk8cgDxMPTgR4UJ3UWicduzNQqSJ3QAz5McRvJ9g5TcdgboCAkvbtxE2FEfJbPtnuZ47gSGq8AeRnvuh8SgMj3NKZdR7Z42oHJwTUQqcqyPTGFuiNR3v34Emp3HfVajSqBxKZLYY8MNwE3JFzKAPPSdnvJahuwgL7JiSSJzn3vvUufz3ur9D1DgbrEAG65hWpDducUyuek1FHFoNUWCDQMCdWZRbeCEYMsLvBFj",
		// Safing Intel Signing Key #2
		"recipient:public-ed25519-key:safing-intel-signing-key-2:cS43veLU6cSnMwJbhE7E47RfsLPWsRSdGtaVnQwK36uwW6ncnR9vqRYHcXqoHFJjdsvBzBtmh47gfSVyPp9y78kc79TKAGsKbptUyvKJneTXj26gYnvuYdzfVEbqN5bm29ahv2B2T5qoQ6NyUjPzwv657fVcGf4GVhHrUMa33Xh7SU133RXR6K8HCa3YbMbk59LjYGZRZvAquuz9AmfraXZpGpkQRgbovVNf12TM3mdLQcVso9JP6UbKA3rMCwivdusb9F",
	}

	// IntelSigningTrustStore is an in-memory trust store with the intel signing keys.
	IntelSigningTrustStore = jess.NewMemTrustStore()
)

func init() {
	loadSigningKeys(BinarySigningTrustStore, BinarySigningKeys)
	loadSigningKeys(IntelSigningTrustStore, IntelSigningKeys)
}

// loadSigningKeys parses keys and stores them in trustStore.
// It panics on any error, because the keys are compiled in.
func loadSigningKeys(trustStore *jess.MemTrustStore, keys []string) {
	for _, signingKey := range keys {
		rcpt, err := jess.RecipientFromTextFormat(signingKey)
		if err != nil {
			panic(err)
		}
		err = trustStore.StoreSignet(rcpt)
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
