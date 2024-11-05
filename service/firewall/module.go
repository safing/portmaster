package firewall

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	_ "github.com/safing/portmaster/service/core"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netquery"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/resolver"
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

var allowedClients stringSliceFlag

type Firewall struct {
	mgr      *mgr.Manager
	instance instance
}

func init() {
	flag.Var(&allowedClients, "allowed-clients", "A list of binaries that are allowed to connect to the Portmaster API")
}

func (f *Firewall) Manager() *mgr.Manager {
	return f.mgr
}

func (f *Firewall) Start() error {
	if err := prep(); err != nil {
		log.Errorf("Failed to prepare firewall module %q", err)
		return err
	}

	return start()
}

func (f *Firewall) Stop() error {
	// Cancel all workers and give them a little time.
	// The bandwidth updater can crash the sqlite DB for some reason.
	// TODO: Investigate.
	f.mgr.Cancel()
	time.Sleep(100 * time.Millisecond)

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
	module.instance.Captain().EventSPNConnected.AddCallback("reset connection verdicts on SPN connect", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
		resetAllConnectionVerdicts()
		return false, err
	})

	// Reset connections when account is updated.
	// This will not change verdicts, but will update the feature flags on connections.
	module.instance.Access().EventAccountUpdate.AddCallback("update connection feature flags after account update", func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
		resetAllConnectionVerdicts()
		return false, err
	})

	module.instance.Network().EventConnectionReattributed.AddCallback("reset connection verdicts after connection re-attribution", func(wc *mgr.WorkerCtx, connID string) (cancel bool, err error) {
		// Expected event data: connection ID.
		resetSingleConnectionVerdict(connID)
		return false, err
	})

	return nil
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
	module     *Firewall
	shimLoaded atomic.Bool
)

func New(instance instance) (*Firewall, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("Firewall")
	module = &Firewall{
		mgr:      m,
		instance: instance,
	}

	if err := prepAPIAuth(); err != nil {
		return nil, err
	}

	if err := registerConfig(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	Config() *config.Config
	Profile() *profile.ProfileModule
	Captain() *captain.Captain
	Access() *access.Access
	Network() *network.Network
	NetQuery() *netquery.NetQuery
	Resolver() *resolver.ResolverModule
}
