package status

import (
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
)

const (
	statusDBKey = "core:status/status"
)

var (
	statusDB = database.NewInterface(nil)
	hook     *database.RegisteredHook
)

type statusHook struct {
	database.HookBase
}

// UsesPrePut implements the Hook interface.
func (sh *statusHook) UsesPrePut() bool {
	return true
}

// PrePut implements the Hook interface.
func (sh *statusHook) PrePut(r record.Record) (record.Record, error) {
	// record is already locked!

	newStatus, err := EnsureSystemStatus(r)
	if err != nil {
		return nil, err
	}

	// apply applicable settings
	if SelectedSecurityLevel() != newStatus.SelectedSecurityLevel {
		go setSelectedSecurityLevel(newStatus.SelectedSecurityLevel)
	}

	// TODO: allow setting of Gate17 status (on/off)

	// return original status
	return status, nil
}

func initStatusHook() (err error) {
	hook, err = database.RegisterHook(query.New(statusDBKey), &statusHook{})
	return err
}
