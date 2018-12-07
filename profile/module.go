package profile

import "github.com/Safing/portbase/modules"

var (
	shutdownSignal = make(chan struct{})
)

func init() {
	modules.Register("profile", nil, start, stop, "database")
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
