package patrol

import (
	"context"
	"fmt"
	"sort"
	"testing"
)

var enableDomainTools = "no" // change to "yes" to enable

// TestCleanDomains checks, cleans and prints an improved domain list.
// Run with:
// go test -run ^TestCleanDomains$ github.com/safing/portmaster/spn/patrol -ldflags "-X github.com/safing/portmaster/spn/patrol.enableDomainTools=yes" -timeout 3h -v
// This is provided as a test for easier maintenance and ops.
func TestCleanDomains(t *testing.T) { //nolint:paralleltest
	if enableDomainTools != "yes" {
		t.Skip()
		return
	}

	// Setup context.
	ctx := context.Background()

	// Go through all domains and check if they are reachable.
	goodDomains := make([]string, 0, len(testDomains))
	for _, domain := range testDomains {
		// Check if domain is reachable.
		code, err := domainIsUsable(ctx, domain)
		if err != nil {
			fmt.Printf("FAIL: %s: %s\n", domain, err)
		} else {
			fmt.Printf("OK: %s [%d]\n", domain, code)
			goodDomains = append(goodDomains, domain)
			continue
		}

		// If failed, try again with a www. prefix
		wwwDomain := "www." + domain
		code, err = domainIsUsable(ctx, wwwDomain)
		if err != nil {
			fmt.Printf("FAIL: %s: %s\n", wwwDomain, err)
		} else {
			fmt.Printf("OK: %s [%d]\n", wwwDomain, code)
			goodDomains = append(goodDomains, wwwDomain)
		}

	}

	sort.Strings(goodDomains)
	fmt.Println("printing good domains:")
	for _, domain := range goodDomains {
		fmt.Printf("%q,\n", domain)
	}

	fmt.Println("IMPORTANT: do not forget to go through list and check if everything looks good")
}

func domainIsUsable(ctx context.Context, domain string) (statusCode int, err error) {
	// Try IPv6 first as it is way more likely to fail.
	statusCode, err = CheckHTTPSConnection(ctx, "tcp6", domain)
	if err != nil {
		return
	}

	return CheckHTTPSConnection(ctx, "tcp4", domain)
}
