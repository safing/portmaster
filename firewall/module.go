package firewall

import (
	"context"
	"fmt"
	"strings"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	_ "github.com/safing/portmaster/core"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/profile"
	"github.com/safing/spn/access"
	"github.com/safing/spn/captain"
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
		nil,
	)
}

func prep() error {
	network.SetDefaultFirewallHandler(verdictHandler)

	// Reset connections every time configuration changes
	// this will be triggered on spn enable/disable
	err := module.RegisterEventHook(
		"config",
		config.ChangeEvent,
		"reset connection verdicts after global config change",
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
		profile.ConfigChangeEvent,
		"reset connection verdicts after profile config change",
		func(ctx context.Context, eventData interface{}) error {
			// Expected event data: scoped profile ID.
			profileID, ok := eventData.(string)
			if !ok {
				return fmt.Errorf("event data is not a string: %v", eventData)
			}
			profileSource, profileID, ok := strings.Cut(profileID, "/")
			if !ok {
				return fmt.Errorf("event data does not seem to be a scoped profile ID: %v", eventData)
			}

			resetProfileConnectionVerdict(profileSource, profileID)
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
		captain.SPNConnectedEvent,
		"reset connection verdicts on SPN connect",
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
		"update connection feature flags after account update",
		func(ctx context.Context, _ interface{}) error {
			resetAllConnectionVerdicts()
			return nil
		},
	)
	if err != nil {
		log.Errorf("filter: failed to register event hook: %s", err)
	}

	err = module.RegisterEventHook(
		"network",
		network.ConnectionReattributedEvent,
		"reset verdict of re-attributed connection",
		func(ctx context.Context, eventData interface{}) error {
			// Expected event data: connection ID.
			connID, ok := eventData.(string)
			if !ok {
				return fmt.Errorf("event data is not a string: %v", eventData)
			}
			resetSingleConnectionVerdict(connID)
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
