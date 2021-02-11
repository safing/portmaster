package status

import (
	"fmt"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/runtime"
	"github.com/safing/portmaster/netenv"
)

var (
	pushUpdate runtime.PushFunc
)

func setupRuntimeProvider() (err error) {
	// register the system status getter
	//
	statusProvider := runtime.SimpleValueGetterFunc(func(_ string) ([]record.Record, error) {
		return []record.Record{buildSystemStatus()}, nil
	})
	pushUpdate, err = runtime.Register("system/status", statusProvider)
	if err != nil {
		return err
	}

	// register the selected security level setter
	//
	levelProvider := runtime.SimpleValueSetterFunc(setSelectedSecurityLevel)
	_, err = runtime.Register("system/security-level", levelProvider)
	if err != nil {
		return err
	}

	return nil
}

// setSelectedSecurityLevel updates the selected security level.
func setSelectedSecurityLevel(r record.Record) (record.Record, error) {
	var upd *SelectedSecurityLevelRecord
	if r.IsWrapped() {
		upd = new(SelectedSecurityLevelRecord)
		if err := record.Unwrap(r, upd); err != nil {
			return nil, err
		}
	} else {
		// TODO(ppacher): this can actually never happen
		// as we're write-only and ValueProvider.Set() should
		// only ever be called from the HTTP API (so r must be wrapped).
		// Though, make sure we handle the case as well ...
		var ok bool
		upd, ok = r.(*SelectedSecurityLevelRecord)
		if !ok {
			return nil, fmt.Errorf("expected *SelectedSecurityLevelRecord but got %T", r)
		}
	}

	if !IsValidSecurityLevel(upd.SelectedSecurityLevel) {
		return nil, fmt.Errorf("invalid security level: %d", upd.SelectedSecurityLevel)
	}

	if SelectedSecurityLevel() != upd.SelectedSecurityLevel {
		setSelectedLevel(upd.SelectedSecurityLevel)
		triggerAutopilot()
	}

	return r, nil
}

// buildSystemStatus build a new system status record.
func buildSystemStatus() *SystemStatusRecord {
	status := &SystemStatusRecord{
		ActiveSecurityLevel:   ActiveSecurityLevel(),
		SelectedSecurityLevel: SelectedSecurityLevel(),
		ThreatMitigationLevel: getHighestMitigationLevel(),
		CaptivePortal:         netenv.GetCaptivePortal(),
		OnlineStatus:          netenv.GetOnlineStatus(),
	}

	status.CreateMeta()
	status.SetKey("runtime:system/status")

	return status
}

// pushSystemStatus pushes a new system status via
// the runtime database.
func pushSystemStatus() {
	if pushUpdate == nil {
		return
	}

	record := buildSystemStatus()
	record.Lock()
	defer record.Unlock()

	pushUpdate(record)
}
