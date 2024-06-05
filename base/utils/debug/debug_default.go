//go:build !android

package debug

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/host"
)

// AddPlatformInfo adds OS and platform information.
func (di *Info) AddPlatformInfo(ctx context.Context) {
	// Get information from the system.
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		di.AddSection(
			"Platform Information",
			NoFlags,
			fmt.Sprintf("Failed to get: %s", err),
		)
		return
	}

	// Check if we want to add virtulization information.
	var virtInfo string
	if info.VirtualizationRole == "guest" {
		if info.VirtualizationSystem != "" {
			virtInfo = fmt.Sprintf("VM: %s", info.VirtualizationSystem)
		} else {
			virtInfo = "VM: unidentified"
		}
	}

	// Add section.
	di.AddSection(
		fmt.Sprintf("Platform: %s %s", info.Platform, info.PlatformVersion),
		UseCodeSection|AddContentLineBreaks,
		fmt.Sprintf("System: %s %s (%s) %s", info.Platform, info.OS, info.PlatformFamily, info.PlatformVersion),
		fmt.Sprintf("Kernel: %s %s", info.KernelVersion, info.KernelArch),
		virtInfo,
	)
}
