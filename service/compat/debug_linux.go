package compat

import (
	"fmt"

	"github.com/safing/portmaster/base/utils/debug"
)

// AddToDebugInfo adds compatibility data to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	// Get iptables state and add error info if it fails.
	chains, err := GetIPTablesChains()
	if err != nil {
		di.AddSection(
			"Compatibility: IPTables Chains (failed)",
			debug.UseCodeSection,
			err.Error(),
		)
		return
	}

	// Add data as section.
	di.AddSection(
		fmt.Sprintf("Compatibility: IPTables Chains (%d)", len(chains)-10),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		chains...,
	)
}
