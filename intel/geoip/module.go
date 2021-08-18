package geoip

import (
	"context"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/updates"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("geoip", prep, nil, nil, "base", "updates")
}

func prep() error {
	return module.RegisterEventHook(
		updates.ModuleName,
		updates.ResourceUpdateEvent,
		"Check for GeoIP database updates",
		func(c context.Context, i interface{}) error {
			worker.triggerUpdate()
			return nil
		},
	)
}
