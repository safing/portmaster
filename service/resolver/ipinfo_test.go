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

func TestIPInfoCNAMETargetQueriedDirectly(t *testing.T) {
	t.Parallel()

	chain := ResolvedDomain{
		Domain: "tr.archive.ubuntu.com.",
		CNAMEs: []string{"ftp.linux.org.tr."},
	}
	target := ResolvedDomain{
		Domain: "ftp.linux.org.tr.",
	}
	other := ResolvedDomain{
		Domain: "other.example.",
	}

	info := &IPInfo{IP: "193.140.98.183"}
	info.AddDomain(other)
	info.AddDomain(chain)
	// The resolver follows the chain and queries the target on its own.
	info.AddDomain(target)

	assert.Equal(t, ResolvedDomains{other, target, chain}, info.ResolvedDomains)
	assert.Equal(t, chain.Domain, info.MostRecentDomain().Domain)

	// A different domain resolved later still becomes the most recent one.
	info.AddDomain(other)
	assert.Equal(t, other.Domain, info.MostRecentDomain().Domain)
}
