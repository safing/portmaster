//go:build !windows && !linux

package compat

import "github.com/safing/portbase/utils/debug"

// AddToDebugInfo adds compatibility data to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	// Not yet implemented on this platform.
}
