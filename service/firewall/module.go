package firewall

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	_ "github.com/safing/portmaster/service/core"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/captain"
)

type stringSliceFlag []string

func (ss *stringSliceFlag) String() string {
	return strings.Join(*ss, ":")
}

func (ss *stringSliceFlag) Set(value string) error {
	*ss = append(*ss, filepath.Clean(value))
	return nil
}

// module         *modules.Module
var allowedClients stringSliceFlag

type Filter struct {
	mgr *mgr.Manager

	instance instance
}

func init() {
	flag.Var(&allowedClients, "allowed-clients", "A list of binaries that are allowed to connect to the Portmaster API")
}

func (f *Filter) Start(mgr *mgr.Manager) error {
	f.mgr = mgr

	if err := prep(); err != nil {
		return err
	}

	return start()
}

func (f *Filter) Stop(mgr *mgr.Manager) error {
	return stop()
}

func prep() error {
	network.SetDefaultFirewallHandler(defaultFirewallHandler)

	// Reset connections every time configuration changes
	// this will be triggered on spn enable/disable
	module.instance.Config().EventConfigChange.AddCallback("reset connection verdicts after global config change", func(w *mgr.WorkerCtx, _ struct{}) (bool, error) {
		resetAllConnectionVerdicts()
		return false, nil
	})

	module.instance.Profile().EventConfigChange.AddCallback("reset connection verdicts after profile config change",
		func(m *mgr.WorkerCtx, profileID string) (bool, error) {
			// Expected event data: scoped profile ID.
			profileSource, profileID, ok := strings.Cut(profileID, "/")
			if !ok {
				return false, fmt.Errorf("event data does not seem to be a scoped profile ID: %v", profileID)
			}

			resetProfileConnectionVerdict(profileSource, profileID)
			return false, nil
		},
	)

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

	module.mgr.Go("packet handler", packetHandler)
	module.mgr.Go("bandwidth update handler", bandwidthUpdateHandler)

	// Start stat logger if logging is set to trace.
	if log.GetLogLevel() == log.TraceLevel {
		module.mgr.Go("stat logger", statLogger)
	}

	return nil
}

func stop() error {
	return nil
}

var (
	module     *Filter
	shimLoaded atomic.Bool
)

func New(instance instance) (*Filter, error) {
	module = &Filter{
		instance: instance,
	}

	return module, nil
}

type instance interface {
	Config() *config.Config
	Profile() *profile.ProfileModule
}
