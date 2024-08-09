package updater

import (
	"strings"

	"github.com/safing/jess"
)

// VerificationOptions holds options for verification of files.
type VerificationOptions struct {
	TrustStore     jess.TrustStore
	DownloadPolicy SignaturePolicy
	DiskLoadPolicy SignaturePolicy
}

// GetVerificationOptions returns the verification options for the given identifier.
func (reg *ResourceRegistry) GetVerificationOptions(identifier string) *VerificationOptions {
	if reg.Verification == nil {
		return nil
	}

	var (
		longestPrefix = -1
		bestMatch     *VerificationOptions
	)
	for prefix, opts := range reg.Verification {
		if len(prefix) > longestPrefix && strings.HasPrefix(identifier, prefix) {
			longestPrefix = len(prefix)
			bestMatch = opts
		}
	}

	return bestMatch
}

// SignaturePolicy defines behavior in case of errors.
type SignaturePolicy uint8

// Signature Policies.
const (
	// SignaturePolicyRequire fails on any error.
	SignaturePolicyRequire = iota

	// SignaturePolicyWarn only warns on errors.
	SignaturePolicyWarn

	// SignaturePolicyDisable only downloads signatures, but does not verify them.
	SignaturePolicyDisable
)
