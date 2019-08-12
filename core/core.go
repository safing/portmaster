package core

import (
	"fmt"

	"github.com/safing/portbase/modules"
)

var (
	coreModule = modules.Register("core", nil, startCore, nil, "base", "database", "config", "api", "random")
)

func startCore() error {
	if err := startPlatformSpecific(); err != nil {
		return fmt.Errorf("failed to start plattform-specific stuff: %s", err)
	}

	return registerDatabases()
}
