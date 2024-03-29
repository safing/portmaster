package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIPInfo(t *testing.T) {
	t.Parallel()

	example := ResolvedDomain{
		Domain: "example.com.",
	}
	subExample := ResolvedDomain{
		Domain: "sub1.example.com",
		CNAMEs: []string{"example.com"},
	}

	info := &IPInfo{
		IP: "1.2.3.4",
		ResolvedDomains: ResolvedDomains{
			example,
			subExample,
		},
	}

	sub2Example := ResolvedDomain{
		Domain: "sub2.example.com",
		CNAMEs: []string{"sub1.example.com", "example.com"},
	}
	info.AddDomain(sub2Example)
	assert.Equal(t, ResolvedDomains{example, subExample, sub2Example}, info.ResolvedDomains)

	// try again, should do nothing now
	info.AddDomain(sub2Example)
	assert.Equal(t, ResolvedDomains{example, subExample, sub2Example}, info.ResolvedDomains)

	subOverWrite := ResolvedDomain{
		Domain: "sub1.example.com",
		CNAMEs: []string{}, // now without CNAMEs
	}

	info.AddDomain(subOverWrite)
	assert.Equal(t, ResolvedDomains{example, sub2Example, subOverWrite}, info.ResolvedDomains)
}
