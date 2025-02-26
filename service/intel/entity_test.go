package intel

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var splitDomainTestCases = [][]string{
	// Regular registered domains and subdomains.
	{"example.com."},
	{"www.example.com.", "example.com."},
	{"sub.domain.example.com.", "domain.example.com.", "example.com."},
	{"example.co.uk."},
	{"www.example.co.uk.", "example.co.uk."},

	// TLD or public suffix: Return as is.
	{"com."},
	{"googleapis.com."},

	// Public suffix domains: Return including public suffix.
	{"test.googleapis.com.", "googleapis.com."},
	{"sub.domain.googleapis.com.", "domain.googleapis.com.", "googleapis.com."},
}

func TestSplitDomain(t *testing.T) {
	t.Parallel()

	for _, testCase := range splitDomainTestCases {
		splitted := splitDomain(testCase[0])
		assert.Equal(t, testCase, splitted, "result must match")
	}
}
