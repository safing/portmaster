//go:build linux

package compat

import (
	"testing"
)

func TestIPTablesChains(t *testing.T) {
	// Skip in CI.
	if testing.Short() {
		t.Skip()
	}
	t.Parallel()

	chain, err := GetIPTablesChains()
	if err != nil {
		t.Fatal(err)
	}

	if len(chain) < 35 {
		t.Errorf("Expected at least 35 output lines, not %d", len(chain))
	}
}
