package updates

import (
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/base/updater"
)

var pushRegistryStatusUpdate runtime.PushFunc

// RegistryStateExport is a wrapper to export the registry state.
type RegistryStateExport struct {
	record.Base
	*updater.RegistryState
}

func exportRegistryState(s *updater.RegistryState) *RegistryStateExport {
	if s == nil {
		state := registry.GetState()
		s = &state
	}

	export := &RegistryStateExport{
		RegistryState: s,
	}

	export.CreateMeta()
	export.SetKey("runtime:core/updates/state")

	return export
}

func pushRegistryState(s *updater.RegistryState) {
	export := exportRegistryState(s)
	pushRegistryStatusUpdate(export)
}

func registerRegistryStateProvider() (err error) {
	registryStateProvider := runtime.SimpleValueGetterFunc(func(_ string) ([]record.Record, error) {
		return []record.Record{exportRegistryState(nil)}, nil
	})

	pushRegistryStatusUpdate, err = runtime.Register("core/updates/state", registryStateProvider)
	if err != nil {
		return err
	}

	return nil
}
