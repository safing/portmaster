package firewall

import (
	"context"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	_ "github.com/safing/portmaster/core"
	"github.com/safing/portmaster/network"
	"github.com/safing/spn/access"
)

var module *modules.Module

func init() {
	module = modules.Register("filter", prep, start, stop, "core", "interception", "intel", "netquery")
	subsystems.Register(
		"filter",
		"Privacy Filter",
		"DNS and Network Filter",
		module,
		"config:filter/",
		&config.Option{
			Name:           "Privacy Filter Module",
			Key:            CfgOptionEnableFilterKey,
			Description:    "Start the Privacy Filter module. If turned off, all privacy filter protections are fully disabled on this device.",
			OptType:        config.OptTypeBool,
			ExpertiseLevel: config.ExpertiseLevelDeveloper,
			ReleaseLevel:   config.ReleaseLevelStable,
			DefaultValue:   true,
			Annotations: config.Annotations{
				config.CategoryAnnotation: "General",
			},
		},
	)
}

const (
	configChangeEvent        = "config change"
	profileConfigChangeEvent = "profile config change"
	onSPNConnectEvent        = "spn connect"
)

func prep() error {
	network.SetDefaultFirewallHandler(verdictHandler)

	// Reset connections every time configuration changes
	// this will be triggered on spn enable/disable
	err := module.RegisterEventHook(
		"config",
		configChangeEvent,
		"reset connection verdicts",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("filter: failed to register event hook: %s", err)
	}

	// Reset connections every time profile changes
	err = module.RegisterEventHook(
		"profiles",
		profileConfigChangeEvent,
		"reset connection verdicts",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("filter: failed to register event hook: %s", err)
	}

	// Reset connections when spn is connected
	// connect and disconnecting is triggered on config change event but connecting takеs more time
	err = module.RegisterEventHook(
		"captain",
		onSPNConnectEvent,
		"reset connection verdicts",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("filter: failed to register event hook: %s", err)
	}

	// Reset connections when account is updated.
	// This will not change verdicts, but will update the feature flags on connections.
	err = module.RegisterEventHook(
		"access",
		access.AccountUpdateEvent,
		"update connection feature flags",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("filter: failed to register event hook: %s", err)
	}

	if err := registerConfig(); err != nil {
		return err
	}

	return prepAPIAuth()
}

func start() error {
	getConfig()
	startAPIAuth()

	module.StartServiceWorker("packet handler", 0, packetHandler)
	module.StartServiceWorker("bandwidth update handler", 0, bandwidthUpdateHandler)

	// Start stat logger if logging is set to trace.
	if log.GetLogLevel() == log.TraceLevel {
		module.StartServiceWorker("stat logger", 0, statLogger)
	}

	return nil
}

func stop() error {
	return nil
}
