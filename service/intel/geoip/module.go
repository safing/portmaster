package geoip

import (
	"errors"
	"sync/atomic"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

type GeoIP struct {
	mgr      *mgr.Manager
	instance instance
}

func (g *GeoIP) Manager() *mgr.Manager {
	return g.mgr
}

func (g *GeoIP) Start() error {
	module.instance.Updates().EventResourcesUpdated.AddCallback(
		"Check for GeoIP database updates",
		func(_ *mgr.WorkerCtx, _ struct{}) (bool, error) {
			worker.triggerUpdate()
			return false, nil
		})
	return nil
}

func (g *GeoIP) Stop() error {
	return nil
}

var (
	module     *GeoIP
	shimLoaded atomic.Bool
)

// New returns a new GeoIP module.
func New(instance instance) (*GeoIP, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("geoip")
	module = &GeoIP{
		mgr:      m,
		instance: instance,
	}

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
		return nil, err
	}

	return module, nil
}

type instance interface {
	Updates() *updates.Updates
}
