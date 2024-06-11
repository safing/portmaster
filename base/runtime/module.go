package runtime

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/service/mgr"
)

// DefaultRegistry is the default registry
// that is used by the module-level API.
var DefaultRegistry = NewRegistry()

type Runtime struct {
	instance instance
}

func (r *Runtime) Start(m *mgr.Manager) error {
	_, err := database.Register(&database.Database{
		Name:         "runtime",
		Description:  "Runtime database",
		StorageType:  "injected",
		ShadowDelete: false,
	})
	if err != nil {
		return err
	}

	if err := DefaultRegistry.InjectAsDatabase("runtime"); err != nil {
		return err
	}

	if err := startModulesIntegration(); err != nil {
		return fmt.Errorf("failed to start modules integration: %w", err)
	}

	return nil
}

func (r *Runtime) Stop(m *mgr.Manager) error {
	return nil
}

// Register is like Registry.Register but uses
// the package DefaultRegistry.
func Register(key string, provider ValueProvider) (PushFunc, error) {
	return DefaultRegistry.Register(key, provider)
}

var (
	module     *Runtime
	shimLoaded atomic.Bool
)

func New(instance instance) (*Runtime, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Runtime{instance: instance}

	return module, nil
}

type instance interface{}
