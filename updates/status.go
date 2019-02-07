package updates

import (
	"errors"
	"sync"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/query"
	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portbase/log"
	"github.com/tevino/abool"
)

// database key for update information
const (
	statusDBKey = "core:status/updates"
)

// version type
type versionClass int

const (
	versionClassLocal versionClass = iota
	versionClassStable
	versionClassBeta
)

// working vars
var (
	status *versionStatus

	statusDB         = database.NewInterface(nil)
	statusHook       *database.RegisteredHook
	enableStatusSave = abool.NewBool(false)
)

func init() {
	status = &versionStatus{
		Versions: make(map[string]*versionStatusEntry),
	}
	status.SetKey(statusDBKey)
}

// versionStatus holds update version status information.
type versionStatus struct {
	record.Base
	sync.Mutex
	Versions map[string]*versionStatusEntry
}

func (vs *versionStatus) save() {
	enableStatusSave.SetTo(true)
	err := statusDB.Put(vs)
	if err != nil {
		log.Warningf("could not save updates version status: %s", err)
	}
}

// versionStatusEntry holds information about the update status of a module.
type versionStatusEntry struct {
	LastVersionUsed string
	LocalVersion    string
	StableVersion   string
	BetaVersion     string
}

func updateUsedStatus(identifier string, version string) {
	status.Lock()
	defer status.Unlock()

	entry, ok := status.Versions[identifier]
	if !ok {
		entry = &versionStatusEntry{}
		status.Versions[identifier] = entry
	}

	entry.LastVersionUsed = version

	log.Tracef("updates: updated last used version of %s: %s", identifier, version)

	go status.save()
}

func updateStatus(vClass versionClass, state map[string]string) {
	status.Lock()
	defer status.Unlock()

	for identifier, version := range state {

		entry, ok := status.Versions[identifier]
		if !ok {
			entry = &versionStatusEntry{}
			status.Versions[identifier] = entry
		}

		switch vClass {
		case versionClassLocal:
			entry.LocalVersion = version
		case versionClassStable:
			entry.StableVersion = version
		case versionClassBeta:
			entry.BetaVersion = version
		}
	}

	go status.save()
}

type updateStatusHook struct {
	database.HookBase
}

// UsesPrePut implements the Hook interface.
func (sh *updateStatusHook) UsesPrePut() bool {
	return true
}

// PrePut implements the Hook interface.
func (sh *updateStatusHook) PrePut(r record.Record) (record.Record, error) {
	if enableStatusSave.SetToIf(true, false) {
		return r, nil
	}
	return nil, errors.New("may only be changed by updates module")
}

func initUpdateStatusHook() (err error) {
	statusHook, err = database.RegisterHook(query.New(statusDBKey), &updateStatusHook{})
	return err
}
