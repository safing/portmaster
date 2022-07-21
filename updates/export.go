package updates

import (
	"context"
	"sync"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates/helper"
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

func initVersionExport() (err error) {
	if err := GetVersions().save(); err != nil {
		log.Warningf("updates: failed to export version information: %s", err)
	}
	if err := GetSimpleVersions().save(); err != nil {
		log.Warningf("updates: failed to export version information: %s", err)
	}

	return module.RegisterEventHook(
		ModuleName,
		VersionUpdateEvent,
		"export version status",
		export,
	)
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
func export(_ context.Context, _ interface{}) error {
	if err := GetVersions().save(); err != nil {
		return err
	}
	return GetSimpleVersions().save()
}
