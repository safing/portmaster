package ui

import (
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

	// Close all archives.
	for id, archiveFS := range apps {
		err := archiveFS.Close()
		if err != nil {
			log.Warningf("ui: failed to close archive %s: %s", id, err)
		}
	}

	// Reset index.
	for key := range apps {
		delete(apps, key)
	}

	return "all ui archives successfully reloaded", nil
}
