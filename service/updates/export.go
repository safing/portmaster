package updates

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates/helper"
)

const (
	// versionsDBKey is the database key for update version information.
	versionsDBKey = "core:status/versions"

	// versionsDBKey is the database key for simple update version information.
	simpleVersionsDBKey = "core:status/simple-versions"

	// updateStatusDBKey is the database key for update status information.
	updateStatusDBKey = "core:status/updates"
)

// Versions holds update versions and status information.
type Versions struct {
	record.Base
	sync.Mutex

	Core      *info.Info
	Resources map[string]*updater.Resource
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

// UpdateStateExport is a wrapper to export the updates state.
type UpdateStateExport struct {
	record.Base
	sync.Mutex

	*updater.UpdateState
}

// GetVersions returns the update versions and status information.
// Resources must be locked when accessed.
func GetVersions() *Versions {
	return &Versions{
		Core:      info.GetInfo(),
		Resources: registry.Export(),
		Channel:   initialReleaseChannel,
		Beta:      initialReleaseChannel == helper.ReleaseChannelBeta,
		Staging:   initialReleaseChannel == helper.ReleaseChannelStaging,
	}
}

// GetSimpleVersions returns the simplified update versions and status information.
func GetSimpleVersions() *SimpleVersions {
	// Fill base info.
	v := &SimpleVersions{
		Build:     info.GetInfo(),
		Resources: make(map[string]*SimplifiedResourceVersion),
		Channel:   initialReleaseChannel,
	}

	// Iterate through all versions and add version info.
	for id, resource := range registry.Export() {
		func() {
			resource.Lock()
			defer resource.Unlock()

			// Get current in-used or selected version.
			var rv *updater.ResourceVersion
			switch {
			case resource.ActiveVersion != nil:
				rv = resource.ActiveVersion
			case resource.SelectedVersion != nil:
				rv = resource.SelectedVersion
			}

			// Get information from resource.
			if rv != nil {
				v.Resources[id] = &SimplifiedResourceVersion{
					Version: rv.VersionNumber,
				}
			}
		}()
	}

	return v
}

// GetStateExport gets the update state from the registry and returns it in an
// exportable struct.
func GetStateExport() *UpdateStateExport {
	export := registry.GetState()
	return &UpdateStateExport{
		UpdateState: &export.Updates,
	}
}

// LoadStateExport loads the exported update state from the database.
func LoadStateExport() (*UpdateStateExport, error) {
	r, err := db.Get(updateStatusDBKey)
	if err != nil {
		return nil, err
	}

	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newRecord := &UpdateStateExport{}
		err = record.Unwrap(r, newRecord)
		if err != nil {
			return nil, err
		}
		return newRecord, nil
	}

	// or adjust type
	newRecord, ok := r.(*UpdateStateExport)
	if !ok {
		return nil, fmt.Errorf("record not of type *UpdateStateExport, but %T", r)
	}
	return newRecord, nil
}

func initVersionExport() (err error) {
	if err := GetVersions().save(); err != nil {
		log.Warningf("updates: failed to export version information: %s", err)
	}
	if err := GetSimpleVersions().save(); err != nil {
		log.Warningf("updates: failed to export version information: %s", err)
	}

	module.EventVersionsUpdated.AddCallback("export version status", export)
	return nil
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

func (s *UpdateStateExport) save() error {
	if !s.KeyIsSet() {
		s.SetKey(updateStatusDBKey)
	}
	return db.Put(s)
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
	// Export udpate state.
	if err := GetStateExport().save(); err != nil {
		return false, err
	}

	return false, nil
}

// AddToDebugInfo adds the update system status to the given debug.Info.
func AddToDebugInfo(di *debug.Info) {
	// Get resources from registry.
	resources := registry.Export()
	platformPrefix := helper.PlatformIdentifier("")

	// Collect data for debug info.
	var active, selected []string
	var activeCnt, totalCnt int
	for id, r := range resources {
		// Ignore resources for other platforms.
		if !strings.HasPrefix(id, "all/") && !strings.HasPrefix(id, platformPrefix) {
			continue
		}

		totalCnt++
		if r.ActiveVersion != nil {
			activeCnt++
			active = append(active, fmt.Sprintf("%s: %s", id, r.ActiveVersion.VersionNumber))
		}
		if r.SelectedVersion != nil {
			selected = append(selected, fmt.Sprintf("%s: %s", id, r.SelectedVersion.VersionNumber))
		}
	}
	sort.Strings(active)
	sort.Strings(selected)

	// Compile to one list.
	lines := make([]string, 0, len(active)+len(selected)+3)
	lines = append(lines, "Active:")
	lines = append(lines, active...)
	lines = append(lines, "")
	lines = append(lines, "Selected:")
	lines = append(lines, selected...)

	// Add section.
	di.AddSection(
		fmt.Sprintf("Updates: %s (%d/%d)", initialReleaseChannel, activeCnt, totalCnt),
		debug.UseCodeSection|debug.AddContentLineBreaks,
		lines...,
	)
}
