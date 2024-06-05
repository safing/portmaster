package runtime

import (
	"fmt"
	"sync"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/modules"
)

var modulesIntegrationUpdatePusher func(...record.Record)

func startModulesIntegration() (err error) {
	modulesIntegrationUpdatePusher, err = Register("modules/", &ModulesIntegration{})
	if err != nil {
		return err
	}

	if !modules.SetEventSubscriptionFunc(pushModuleEvent) {
		log.Warningf("runtime: failed to register the modules event subscription function")
	}

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
	return nil, ErrReadOnly
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
