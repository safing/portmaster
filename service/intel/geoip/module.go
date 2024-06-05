package geoip

import (
	"context"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/modules"
	"github.com/safing/portmaster/service/updates"
)

var module *modules.Module

func init() {
	module = modules.Register("geoip", prep, nil, nil, "base", "updates")
}

func prep() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "intel/geoip/countries",
		Read: api.PermitUser,
		// Do not attach to module, as the data is always available anyway.
		StructFunc: func(ar *api.Request) (i interface{}, err error) {
			return countries, nil
		},
		Name:        "Get Country Information",
		Description: "Returns a map of country information centers indexed by ISO-A2 country code",
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
