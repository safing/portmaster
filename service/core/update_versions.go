package core

import (
	"bytes"
	"fmt"
	"sync"
	"text/tabwriter"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

const (
	// versionsDBKey is the database key for update version information.
	versionsDBKey = "core:status/versions"

	// versionsDBKey is the database key for simple update version information.
	simpleVersionsDBKey = "core:status/simple-versions"
)

// Versions holds update versions and status information.
type Versions struct {
	record.Base
	sync.Mutex

	Core      *info.Info
	Resources map[string]*updates.Artifact
	Channel   string
	Beta      bool
	Staging   bool
}

// SimpleVersions holds simplified update versions and status information.
type SimpleVersions struct {
	record.Base
	sync.Mutex

	Build     *info.Info
	Resources map[string]*SimplifiedResourceVersion
	Channel   string
}

// SimplifiedResourceVersion holds version information about one resource.
type SimplifiedResourceVersion struct {
	Version string
}

// GetVersions returns the update versions and status information.
// Resources must be locked when accessed.
func GetVersions() *Versions {
	// Get all artifacts.
	resources := make(map[string]*updates.Artifact)
	if artifacts, err := module.instance.BinaryUpdates().GetFiles(); err == nil {
		for _, artifact := range artifacts {
			resources[artifact.Filename] = artifact
		}
	}
	if artifacts, err := module.instance.IntelUpdates().GetFiles(); err == nil {
		for _, artifact := range artifacts {
			resources[artifact.Filename] = artifact
		}
	}

	return &Versions{
		Core:      info.GetInfo(),
		Resources: resources,
		Channel:   initialReleaseChannel,
		Beta:      initialReleaseChannel == ReleaseChannelBeta,
		Staging:   initialReleaseChannel == ReleaseChannelStaging,
	}
}

// GetSimpleVersions returns the simplified update versions and status information.
func GetSimpleVersions() *SimpleVersions {
	// Get all artifacts, simply map.
	resources := make(map[string]*SimplifiedResourceVersion)
	if artifacts, err := module.instance.BinaryUpdates().GetFiles(); err == nil {
		for _, artifact := range artifacts {
			resources[artifact.Filename] = &SimplifiedResourceVersion{
				Version: artifact.Version,
			}
		}
	}
	if artifacts, err := module.instance.IntelUpdates().GetFiles(); err == nil {
		for _, artifact := range artifacts {
			resources[artifact.Filename] = &SimplifiedResourceVersion{
				Version: artifact.Version,
			}
		}
	}

	// Fill base info.
	return &SimpleVersions{
		Build:     info.GetInfo(),
		Resources: resources,
		Channel:   initialReleaseChannel,
	}
}

func initVersionExport() {
	module.instance.BinaryUpdates().EventResourcesUpdated.AddCallback("export version status", export)
	module.instance.IntelUpdates().EventResourcesUpdated.AddCallback("export version status", export)

	_, _ = export(nil, struct{}{})
}

func (v *Versions) save() error {
	if !v.KeyIsSet() {
		v.SetKey(versionsDBKey)
	}
	return db.Put(v)
}

func (v *SimpleVersions) save() error {
	if !v.KeyIsSet() {
		v.SetKey(simpleVersionsDBKey)
	}
	return db.Put(v)
}

// export is an event hook.
func export(_ *mgr.WorkerCtx, _ struct{}) (cancel bool, err error) {
	// Export versions.
	if err := GetVersions().save(); err != nil {
		return false, err
	}
	if err := GetSimpleVersions().save(); err != nil {
		return false, err
	}

	return false, nil
}

// AddVersionsToDebugInfo adds the update system status to the given debug.Info.
func AddVersionsToDebugInfo(di *debug.Info) {
	overviewBuf := bytes.NewBuffer(nil)
	tableBuf := bytes.NewBuffer(nil)
	tabWriter := tabwriter.NewWriter(tableBuf, 8, 4, 3, ' ', 0)
	fmt.Fprint(tabWriter, "\nFile\tVersion\tIndex\tSHA256\n")

	// Collect data for debug info.
	var cnt int
	if index, err := module.instance.BinaryUpdates().GetIndex(); err == nil {
		fmt.Fprintf(overviewBuf, "Binaries Index: v%s from %s\n", index.Version, index.Published)
		for _, artifact := range index.Artifacts {
			fmt.Fprintf(tabWriter, "\n%s\t%s\t%s\t%s", artifact.Filename, vStr(artifact.Version), "binaries", artifact.SHA256)
			cnt++
		}
	}
	if index, err := module.instance.IntelUpdates().GetIndex(); err == nil {
		fmt.Fprintf(overviewBuf, "Intel Index: v%s from %s\n", index.Version, index.Published)
		for _, artifact := range index.Artifacts {
			fmt.Fprintf(tabWriter, "\n%s\t%s\t%s\t%s", artifact.Filename, vStr(artifact.Version), "intel", artifact.SHA256)
			cnt++
		}
	}
	_ = tabWriter.Flush()

	// Add section.
	di.AddSection(
		fmt.Sprintf("Updates: %s (%d)", initialReleaseChannel, cnt),
		debug.UseCodeSection,
		overviewBuf.String(),
		tableBuf.String(),
	)
}

func vStr(v string) string {
	if v != "" {
		return v
	}
	return "unknown"
}
