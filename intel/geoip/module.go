package geoip

import (
	"context"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/updates"
)

var module *modules.Module

func init() {
	module = modules.Register("geoip", prep, nil, nil, "base", "updates")
}

func prep() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "intel/geoip/country-centers",
		Read: api.PermitUser,
		// Do not attach to module, as the data is always available anyway.
		StructFunc: func(ar *api.Request) (i interface{}, err error) {
			return countryCoordinates, nil
		},
		Name:        "Get Geographic Country Centers",
		Description: "Returns a map of country centers indexed by ISO-A2 country code",
	}); err != nil {
		return err
	}

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
