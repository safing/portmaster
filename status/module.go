package status

import (
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("status", nil, start, stop, "base")
}

func start() error {
	err := initSystemStatus()
	if err != nil {
		return err
	}

	err = startNetEnvHooking()
	if err != nil {
		return err
	}

	status.Save()

	return initStatusHook()
}

func initSystemStatus() error {
	// load status from database
	r, err := statusDB.Get(statusDBKey)
	switch err {
	case nil:
		loadedStatus, err := EnsureSystemStatus(r)
		if err != nil {
			log.Criticalf("status: failed to unwrap system status: %s", err)
		} else {
			status = loadedStatus
		}
	case database.ErrNotFound:
		// create new status
	default:
		log.Criticalf("status: failed to load system status: %s", err)
	}

	status.Lock()
	defer status.Unlock()

	// load status into atomic getters
	atomicUpdateSelectedSecurityLevel(status.SelectedSecurityLevel)

	// update status
	status.updateThreatMitigationLevel()
	status.autopilot()
	status.updateOnlineStatus()

	return nil
}

func stop() error {
	return stopStatusHook()
}
