package profile

import (
	"github.com/safing/portbase/modules"

	// module dependencies
	_ "github.com/safing/portmaster/core"
)

var (
	shutdownSignal = make(chan struct{})
)

func init() {
	modules.Register("profile", nil, start, stop, "core")
}

func start() error {
	err := initSpecialProfiles()
	if err != nil {
		return err
	}
	return initUpdateListener()
}

func stop() error {
	close(shutdownSignal)
	return nil
}
