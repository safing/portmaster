package service

import (
	"fmt"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/ui"
)

// Instance is an instance of a mycoria router.
type Instance struct {
	*mgr.Group

	version string

	api *api.API
	ui  *ui.UI
}

// New returns a new mycoria router instance.
func New(version string) (*Instance, error) {
	// Create instance to pass it to modules.
	instance := &Instance{
		version: version,
	}

	var err error
	instance.ui, err = ui.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create ui module: %w", err)
	}

	// Add all modules to instance group.
	instance.Group = mgr.NewGroup(
		instance.ui,
	)

	return instance, nil
}

// Version returns the version.
func (i *Instance) Version() string {
	return i.version
}

// API returns the api module.
func (i *Instance) API() *api.API {
	return i.api
}

// UI returns the ui module.
func (i *Instance) UI() *ui.UI {
	return i.ui
}
