package runtime

import (
	"fmt"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/modules"
)

// DefaultRegistry is the default registry
// that is used by the module-level API.
var DefaultRegistry = NewRegistry()

func init() {
	modules.Register("runtime", nil, startModule, nil, "database")
}

func startModule() error {
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

// Register is like Registry.Register but uses
// the package DefaultRegistry.
func Register(key string, provider ValueProvider) (PushFunc, error) {
	return DefaultRegistry.Register(key, provider)
}
