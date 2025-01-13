package info

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

var (
	name    string
	license string

	version       = "dev build"
	versionNumber = "0.0.0"
	buildSource   = "unknown"
	buildTime     = "unknown"

	info     *Info
	loadInfo sync.Once
)

func init() {
	// Replace space placeholders.
	buildSource = strings.ReplaceAll(buildSource, "_", " ")
	buildTime = strings.ReplaceAll(buildTime, "_", " ")

	// Convert version string from git tag to expected format.
	version = strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(version, "v"), "_", " "))
	versionNumber = strings.TrimSpace(strings.TrimSuffix(version, "dev build"))
	if versionNumber == "" {
		versionNumber = "0.0.0"
	}

	// Get build info.
	buildInfo, _ := debug.ReadBuildInfo()
	buildSettings := make(map[string]string)
	for _, setting := range buildInfo.Settings {
		buildSettings[setting.Key] = setting.Value
	}

	// Add "dev build" to version if repo is dirty.
	if buildSettings["vcs.modified"] == "true" &&
		!strings.HasSuffix(version, "dev build") {
		version += " dev build"
	}
}

// Info holds the programs meta information.
type Info struct { //nolint:maligned
	Name          string
	Version       string
	VersionNumber string
	License       string

	Source    string
	BuildTime string
	CGO       bool

	Commit     string
	CommitTime string
	Dirty      bool

	debug.BuildInfo
}

// Set sets meta information via the main routine. This should be the first thing your program calls.
func Set(setName string, setVersion string, setLicenseName string) {
	name = setName
	license = setLicenseName

	if setVersion != "" {
		version = setVersion
		versionNumber = setVersion
	}
}

// GetInfo returns all the meta information about the program.
func GetInfo() *Info {
	loadInfo.Do(func() {
		buildInfo, _ := debug.ReadBuildInfo()
		buildSettings := make(map[string]string)
		for _, setting := range buildInfo.Settings {
			buildSettings[setting.Key] = setting.Value
		}

		info = &Info{
			Name:          name,
			Version:       version,
			VersionNumber: versionNumber,
			License:       license,
			Source:        buildSource,
			BuildTime:     buildTime,
			CGO:           buildSettings["CGO_ENABLED"] == "1",
			Commit:        buildSettings["vcs.revision"],
			CommitTime:    buildSettings["vcs.time"],
			Dirty:         buildSettings["vcs.modified"] == "true",
			BuildInfo:     *buildInfo,
		}

		if info.Commit == "" {
			info.Commit = "unknown"
		}
		if info.CommitTime == "" {
			info.CommitTime = "unknown"
		}
	})

	return info
}

// Version returns the annotated version.
func Version() string {
	return version
}

// VersionNumber returns the version number only.
func VersionNumber() string {
	return versionNumber
}

// FullVersion returns the full and detailed version string.
func FullVersion() string {
	info := GetInfo()
	builder := new(strings.Builder)

	// Name and version.
	builder.WriteString(fmt.Sprintf("%s %s\n", info.Name, version))

	// Build info.
	cgoInfo := "-cgo"
	if info.CGO {
		cgoInfo = "+cgo"
	}
	builder.WriteString(fmt.Sprintf("\nbuilt with %s (%s %s) for %s/%s\n", runtime.Version(), runtime.Compiler, cgoInfo, runtime.GOOS, runtime.GOARCH))
	builder.WriteString(fmt.Sprintf("  at %s\n", info.BuildTime))

	// Commit info.
	dirtyInfo := "clean"
	if info.Dirty {
		dirtyInfo = "dirty"
	}
	builder.WriteString(fmt.Sprintf("\ncommit %s (%s)\n", info.Commit, dirtyInfo))
	builder.WriteString(fmt.Sprintf("  at %s\n", info.CommitTime))
	builder.WriteString(fmt.Sprintf("  from %s\n", info.Source))

	builder.WriteString(fmt.Sprintf("\nLicensed under the %s license.", license))

	return builder.String()
}

// CheckVersion checks if the metadata is ok.
func CheckVersion() error {
	switch {
	case strings.HasSuffix(os.Args[0], ".test"):
		return nil // testing on linux/darwin
	case strings.HasSuffix(os.Args[0], ".test.exe"):
		return nil // testing on windows
	default:
		// check version information
		if name == "" || license == "" {
			return errors.New("must call SetInfo() before calling CheckVersion()")
		}
	}

	return nil
}
