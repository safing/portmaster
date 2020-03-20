package core

import (
	"fmt"

	"github.com/safing/portbase/modules"
)

func init() {
	modules.Register("core", nil, startCore, nil, "database", "config", "api", "random")
}

func startCore() error {
	if err := startPlatformSpecific(); err != nil {
		return fmt.Errorf("failed to start plattform-specific components: %s", err)
	}

	return registerDatabases()
}
