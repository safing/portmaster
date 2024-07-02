package compat

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/utils/debug"
)

// AddToDebugInfo adds compatibility data to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	// Get WFP state and add error info if it fails.
	wfp, err := GetWFPState()
	if err != nil {
		di.AddSection(
			"Compatibility: WFP State (failed)",
			debug.UseCodeSection,
			err.Error(),
		)
		return
	}

	// Add data as section.
	wfpTable := wfp.AsTable()
	di.AddSection(
		fmt.Sprintf("Compatibility: WFP State (%d)", strings.Count(wfpTable, "\n")),
		debug.UseCodeSection,
		wfpTable,
	)
}
