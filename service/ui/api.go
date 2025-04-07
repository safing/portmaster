package ui

import (
	"github.com/safing/portmaster/base/api"
)

func (ui *UI) registerAPIEndpoints() error {
	return api.RegisterEndpoint(api.Endpoint{
		Path:        "ui/reload",
		Write:       api.PermitUser,
		ActionFunc:  ui.reloadUI,
		Name:        "Reload UI Assets",
		Description: "Removes all assets from the cache and reloads the current (possibly updated) version from disk when requested.",
	})
}

func (ui *UI) reloadUI(_ *api.Request) (msg string, err error) {
	// Close all archives.
	ui.CloseArchives()

	return "all ui archives successfully reloaded", nil
}
