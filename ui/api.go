package ui

import (
	resources "github.com/cookieo9/resources-go"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/log"
)

func registerAPIEndpoints() error {
	return api.RegisterEndpoint(api.Endpoint{
		Path:        "ui/reload",
		Write:       api.PermitUser,
		BelongsTo:   module,
		ActionFunc:  reloadUI,
		Name:        "Reload UI Assets",
		Description: "Removes all assets from the cache and reloads the current (possibly updated) version from disk when requested.",
	})
}

func reloadUI(_ *api.Request) (msg string, err error) {
	appsLock.Lock()
	defer appsLock.Unlock()

	// close all bundles.
	for id, bundle := range apps {
		err := bundle.Close()
		if err != nil {
			log.Warningf("ui: failed to close bundle %s: %s", id, err)
		}
	}

	// Reset index.
	apps = make(map[string]*resources.BundleSequence)

	return "all ui bundles successfully reloaded", nil
}
