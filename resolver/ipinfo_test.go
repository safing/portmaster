package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIPInfo(t *testing.T) {
	example := ResolvedDomain{
		Domain: "example.com.",
	}
	subExample := ResolvedDomain{
		Domain: "sub1.example.com",
		CNAMEs: []string{"example.com"},
	}

	ipi := &IPInfo{
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
	added := ipi.AddDomain(sub2Example)

	assert.True(t, added)
	assert.Equal(t, ResolvedDomains{example, subExample, sub2Example}, ipi.ResolvedDomains)

	// try again, should do nothing now
	added = ipi.AddDomain(sub2Example)
	assert.False(t, added)
	assert.Equal(t, ResolvedDomains{example, subExample, sub2Example}, ipi.ResolvedDomains)

	subOverWrite := ResolvedDomain{
		Domain: "sub1.example.com",
		CNAMEs: []string{}, // now without CNAMEs
	}

	added = ipi.AddDomain(subOverWrite)
	assert.True(t, added)
	assert.Equal(t, ResolvedDomains{example, sub2Example, subOverWrite}, ipi.ResolvedDomains)
}
