package core

import (
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/mgr"
)

var modulesIntegrationUpdatePusher func(...record.Record)

func initModulesIntegration() (err error) {
	modulesIntegrationUpdatePusher, err = runtime.Register("modules/", &ModulesIntegration{})
	if err != nil {
		return err
	}

	// Push events via API.
	module.EventRestart.AddCallback("expose restart event", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
		// Send event as runtime:modules/core/event/restart
		pushModuleEvent("core", "restart", false, nil)
		return false, nil
	})
	module.EventShutdown.AddCallback("expose shutdown event", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
		// Send event as runtime:modules/core/event/shutdown
		pushModuleEvent("core", "shutdown", false, nil)
		return false, nil
	})

	return nil
}

// ModulesIntegration provides integration with the modules system.
type ModulesIntegration struct{}

// Set is called when the value is set from outside.
// If the runtime value is considered read-only ErrReadOnly
// should be returned. It is guaranteed that the key of
// the record passed to Set is prefixed with the key used
// to register the value provider.
func (mi *ModulesIntegration) Set(record.Record) (record.Record, error) {
	return nil, runtime.ErrReadOnly
}

// Get should return one or more records that match keyOrPrefix.
// keyOrPrefix is guaranteed to be at least the prefix used to
// register the ValueProvider.
func (mi *ModulesIntegration) Get(keyOrPrefix string) ([]record.Record, error) {
	return nil, database.ErrNotFound
}

type eventData struct {
	record.Base
	sync.Mutex
	Data interface{}
}

func pushModuleEvent(moduleName, eventName string, internal bool, data interface{}) {
	// Create event record and set key.
	eventRecord := &eventData{
		Data: data,
	}
	eventRecord.SetKey(fmt.Sprintf(
		"runtime:modules/%s/event/%s",
		moduleName,
		eventName,
	))
	eventRecord.UpdateMeta()
	if internal {
		eventRecord.Meta().MakeSecret()
		eventRecord.Meta().MakeCrownJewel()
	}

	// Push event to database subscriptions.
	modulesIntegrationUpdatePusher(eventRecord)
}
