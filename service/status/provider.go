package status

import (
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/runtime"
	"github.com/safing/portmaster/service/netenv"
)

var pushUpdate runtime.PushFunc

func setupRuntimeProvider() (err error) {
	// register the system status getter
	statusProvider := runtime.SimpleValueGetterFunc(func(_ string) ([]record.Record, error) {
		return []record.Record{buildSystemStatus()}, nil
	})
	pushUpdate, err = runtime.Register("system/status", statusProvider)
	if err != nil {
		return err
	}

	return nil
}

// buildSystemStatus build a new system status record.
func buildSystemStatus() *SystemStatusRecord {
	status := &SystemStatusRecord{
		CaptivePortal: netenv.GetCaptivePortal(),
		OnlineStatus:  netenv.GetOnlineStatus(),
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
