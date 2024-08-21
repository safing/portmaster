package debug

// TODO: Re-enable Android interfaces.
// Deactived for transition to new module system.

// import (
// 	"context"
// 	"fmt"

// 	"github.com/safing/portmaster-android/go/app_interface"
// )

// // AddPlatformInfo adds OS and platform information.
// func (di *Info) AddPlatformInfo(_ context.Context) {
// 	// Get information from the system.
// 	info, err := app_interface.GetPlatformInfo()
// 	if err != nil {
// 		di.AddSection(
// 			"Platform Information",
// 			NoFlags,
// 			fmt.Sprintf("Failed to get: %s", err),
// 		)
// 		return
// 	}

// 	// Add section.
// 	di.AddSection(
// 		fmt.Sprintf("Platform: Android"),
// 		UseCodeSection|AddContentLineBreaks,
// 		fmt.Sprintf("SDK: %d", info.SDK),
// 		fmt.Sprintf("Device: %s %s (%s)", info.Manufacturer, info.Brand, info.Board),
// 		fmt.Sprintf("App: %s: %s %s", info.ApplicationID, info.VersionName, info.BuildType))

// }
