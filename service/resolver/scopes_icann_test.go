package resolver

import (
	"strings"
	"testing"

	"golang.org/x/net/publicsuffix"
)

// TestSpecialServiceDomainsNotICANN checks that no special-service domain
// (blocked by "Block Unofficial TLDs") is an official ICANN TLD.
func TestSpecialServiceDomainsNotICANN(t *testing.T) {
	t.Parallel()

	for _, d := range specialServiceDomains {
		tld := strings.Trim(d, ".")
		if tld == "onion" { // RFC 7686 special-use name, listed as ICANN in the PSL.
			continue
		}
		if _, icann := publicsuffix.PublicSuffix("example." + tld); icann {
			t.Errorf("%s is an ICANN TLD, remove it from specialServiceDomains", d)
		}
	}
}
