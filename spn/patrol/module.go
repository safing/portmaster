package patrol

import (
	"time"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/spn/conf"
)

// ChangeSignalEventName is the name of the event that signals any change in the patrol system.
const ChangeSignalEventName = "change signal"

var module *modules.Module

func init() {
	module = modules.Register("patrol", prep, start, nil, "rng")
}

func prep() error {
	module.RegisterEvent(ChangeSignalEventName, false)

	return nil
}

func start() error {
	if conf.PublicHub() {
		module.NewTask("connectivity test", connectivityCheckTask).
			Repeat(5 * time.Minute)
	}

	return nil
}
