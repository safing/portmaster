package updates

import (
	"context"
	"errors"
	"sync"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/updates/helper"
)

// Database key for update information.
const (
	versionsDBKey = "core:status/versions"
)

var (
	versionExport   *versions
	versionExportDB = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})
	versionExportHook *database.RegisteredHook
)

// versions holds updates status information.
type versions struct {
	record.Base
	lock sync.Mutex

	Core      *info.Info
	Resources map[string]*updater.Resource
	Channel   string
	Beta      bool
	Staging   bool

	internalSave bool
}

func initVersionExport() (err error) {
	// init export struct
	versionExport = &versions{
		internalSave: true,
		Channel:      initialReleaseChannel,
		Beta:         initialReleaseChannel == helper.ReleaseChannelBeta,
		Staging:      initialReleaseChannel == helper.ReleaseChannelStaging,
	}
	versionExport.SetKey(versionsDBKey)

	// attach hook to database
	versionExportHook, err = database.RegisterHook(query.New(versionsDBKey), &exportHook{})
	if err != nil {
		return err
	}

	return module.RegisterEventHook(
		ModuleName,
		VersionUpdateEvent,
		"export version status",
		export,
	)
}

func stopVersionExport() error {
	return versionExportHook.Cancel()
}

// export is an event hook.
func export(_ context.Context, _ interface{}) error {
	// populate
	versionExport.lock.Lock()
	versionExport.Core = info.GetInfo()
	versionExport.Resources = registry.Export()
	versionExport.lock.Unlock()

	// save
	err := versionExportDB.Put(versionExport)
	if err != nil {
		log.Warningf("updates: failed to export versions: %s", err)
	}

	return nil
}

// Lock locks the versionExport and all associated resources.
func (v *versions) Lock() {
	// lock self
	v.lock.Lock()

	// lock all resources
	for _, res := range v.Resources {
		res.Lock()
	}
}

// Lock unlocks the versionExport and all associated resources.
func (v *versions) Unlock() {
	// unlock all resources
	for _, res := range v.Resources {
		res.Unlock()
	}

	// unlock self
	v.lock.Unlock()
}

type exportHook struct {
	database.HookBase
}

// UsesPrePut implements the Hook interface.
func (eh *exportHook) UsesPrePut() bool {
	return true
}

var errInternalRecord = errors.New("may not modify internal record")

// PrePut implements the Hook interface.
func (eh *exportHook) PrePut(r record.Record) (record.Record, error) {
	if r.IsWrapped() {
		return nil, errInternalRecord
	}
	ve, ok := r.(*versions)
	if !ok {
		return nil, errInternalRecord
	}
	if !ve.internalSave {
		return nil, errInternalRecord
	}
	return r, nil
}
