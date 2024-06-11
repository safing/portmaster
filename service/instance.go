package service

import (
	"fmt"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/firewall"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/status"
	"github.com/safing/portmaster/service/ui"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/captain"
	"github.com/safing/portmaster/spn/crew"
	"github.com/safing/portmaster/spn/docks"
)

// Instance is an instance of a portmaste service.
type Instance struct {
	*mgr.Group

	version string

	api           *api.API
	config        *config.Config
	metrics       *metrics.Metrics
	runtime       *runtime.Runtime
	notifications *notifications.Notifications
	rng           *rng.Rng

	access  *access.Access
	cabin   *cabin.Cabin
	captain *captain.Captain
	crew    *crew.Crew
	docks   *docks.Docks

	updates *updates.Updates
	ui      *ui.UI
	profile *profile.ProfileModule
	filter  *firewall.Filter
	netenv  *netenv.NetEnv
	status  *status.Status
}

// New returns a new portmaster service instance.
func New(version string, svcCfg *ServiceConfig) (*Instance, error) {
	// Create instance to pass it to modules.
	instance := &Instance{
		version: version,
	}

	var err error

	// Base modules
	instance.config, err = config.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create config module: %w", err)
	}
	instance.api, err = api.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create api module: %w", err)
	}
	instance.metrics, err = metrics.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create metrics module: %w", err)
	}
	instance.runtime, err = runtime.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create runtime module: %w", err)
	}
	instance.notifications, err = notifications.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create runtime module: %w", err)
	}
	instance.rng, err = rng.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create rng module: %w", err)
	}

	// SPN modules
	instance.access, err = access.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create access module: %w", err)
	}
	instance.cabin, err = cabin.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create cabin module: %w", err)
	}
	instance.captain, err = captain.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create captain module: %w", err)
	}
	instance.crew, err = crew.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create crew module: %w", err)
	}
	instance.docks, err = docks.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create docks module: %w", err)
	}

	// Service modules
	instance.updates, err = updates.New(instance, svcCfg.ShutdownFunc)
	if err != nil {
		return nil, fmt.Errorf("create updates module: %w", err)
	}
	instance.ui, err = ui.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create ui module: %w", err)
	}
	instance.profile, err = profile.NewModule(instance)
	if err != nil {
		return nil, fmt.Errorf("create profile module: %w", err)
	}
	instance.filter, err = firewall.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create filter module: %w", err)
	}
	instance.netenv, err = netenv.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create netenv module: %w", err)
	}
	instance.status, err = status.New(instance)
	if err != nil {
		return nil, fmt.Errorf("create status module: %w", err)
	}

	// Add all modules to instance group.
	instance.Group = mgr.NewGroup(
		instance.config,
		instance.api,
		instance.metrics,
		instance.runtime,
		instance.notifications,
		instance.rng,

		instance.access,
		instance.cabin,
		instance.captain,
		instance.crew,
		instance.docks,

		instance.updates,
		instance.ui,
		instance.profile,
		instance.filter,
		instance.netenv,
		instance.status,
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

// Metrics returns the metrics module.
func (i *Instance) Metrics() *metrics.Metrics {
	return i.metrics
}

// Runtime returns the runtime module.
func (i *Instance) Runtime() *runtime.Runtime {
	return i.runtime
}

// Notifications returns the notifications module.
func (i *Instance) Notifications() *notifications.Notifications {
	return i.notifications
}

// Rng returns the rng module.
func (i *Instance) Rng() *rng.Rng {
	return i.rng
}

// Access returns the access module.
func (i *Instance) Access() *access.Access {
	return i.access
}

// Cabin returns the cabin module.
func (i *Instance) Cabin() *cabin.Cabin {
	return i.cabin
}

// Captain returns the captain module.
func (i *Instance) Captain() *captain.Captain {
	return i.captain
}

// Crew returns the crew module.
func (i *Instance) Crew() *crew.Crew {
	return i.crew
}

// Crew returns the crew module.
func (i *Instance) Docks() *docks.Docks {
	return i.docks
}

// UI returns the ui module.
func (i *Instance) UI() *ui.UI {
	return i.ui
}

// Config returns the config module.
func (i *Instance) Config() *config.Config {
	return i.config
}

// Profile returns the profile module.
func (i *Instance) Profile() *profile.ProfileModule {
	return i.profile
}

// Profile returns the profile module.
func (i *Instance) Firewall() *firewall.Filter {
	return i.filter
}

// NetEnv returns the netenv module.
func (i *Instance) NetEnv() *netenv.NetEnv {
	return i.netenv
}

// Status returns the status module.
func (i *Instance) Status() *status.Status {
	return i.status
}
