package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/broadcasts"
	"github.com/safing/portmaster/service/compat"
	"github.com/safing/portmaster/service/core"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/firewall"
	"github.com/safing/portmaster/service/firewall/interception"
	"github.com/safing/portmaster/service/firewall/interception/dnsmonitor"
	"github.com/safing/portmaster/service/integration"
	"github.com/safing/portmaster/service/intel/customlists"
	"github.com/safing/portmaster/service/intel/filterlists"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/nameserver"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/netquery"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/resolver"
	"github.com/safing/portmaster/service/status"
	"github.com/safing/portmaster/service/sync"
	"github.com/safing/portmaster/service/ui"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/access"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/captain"
	"github.com/safing/portmaster/spn/crew"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/patrol"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/sluice"
	"github.com/safing/portmaster/spn/terminal"
)

// Instance is an instance of a Portmaster service.
type Instance struct {
	ctx          context.Context
	cancelCtx    context.CancelFunc
	serviceGroup *mgr.Group

	exitCode atomic.Int32

	database      *dbmodule.DBModule
	config        *config.Config
	api           *api.API
	metrics       *metrics.Metrics
	runtime       *runtime.Runtime
	notifications *notifications.Notifications
	rng           *rng.Rng
	base          *base.Base

	core         *core.Core
	updates      *updates.Updates
	integration  *integration.OSIntegration
	geoip        *geoip.GeoIP
	netenv       *netenv.NetEnv
	ui           *ui.UI
	profile      *profile.ProfileModule
	network      *network.Network
	netquery     *netquery.NetQuery
	firewall     *firewall.Firewall
	filterLists  *filterlists.FilterLists
	interception *interception.Interception
	dnsmonitor   *dnsmonitor.DNSMonitor
	customlist   *customlists.CustomList
	status       *status.Status
	broadcasts   *broadcasts.Broadcasts
	compat       *compat.Compat
	nameserver   *nameserver.NameServer
	process      *process.ProcessModule
	resolver     *resolver.ResolverModule
	sync         *sync.Sync

	access *access.Access

	// SPN modules
	SpnGroup  *mgr.ExtendedGroup
	cabin     *cabin.Cabin
	navigator *navigator.Navigator
	captain   *captain.Captain
	crew      *crew.Crew
	docks     *docks.Docks
	patrol    *patrol.Patrol
	ships     *ships.Ships
	sluice    *sluice.SluiceModule
	terminal  *terminal.TerminalModule

	CommandLineOperation func() error
}

// New returns a new Portmaster service instance.
func New(svcCfg *ServiceConfig) (*Instance, error) { //nolint:maintidx
	// Create instance to pass it to modules.
	instance := &Instance{}
	instance.ctx, instance.cancelCtx = context.WithCancel(context.Background())

	var err error
	// Base modules
	instance.base, err = base.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create base module: %w", err)
	}
	instance.database, err = dbmodule.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create database module: %w", err)
	}
	instance.config, err = config.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create config module: %w", err)
	}
	instance.api, err = api.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create api module: %w", err)
	}
	instance.metrics, err = metrics.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create metrics module: %w", err)
	}
	instance.runtime, err = runtime.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create runtime module: %w", err)
	}
	instance.notifications, err = notifications.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create runtime module: %w", err)
	}
	instance.rng, err = rng.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create rng module: %w", err)
	}

	// Service modules
	instance.core, err = core.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create core module: %w", err)
	}
	instance.updates, err = updates.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create updates module: %w", err)
	}
	instance.integration, err = integration.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create integration module: %w", err)
	}
	instance.geoip, err = geoip.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create customlist module: %w", err)
	}
	instance.netenv, err = netenv.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create netenv module: %w", err)
	}
	instance.ui, err = ui.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create ui module: %w", err)
	}
	instance.profile, err = profile.NewModule(instance)
	if err != nil {
		return instance, fmt.Errorf("create profile module: %w", err)
	}
	instance.network, err = network.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create network module: %w", err)
	}
	instance.netquery, err = netquery.NewModule(instance)
	if err != nil {
		return instance, fmt.Errorf("create netquery module: %w", err)
	}
	instance.firewall, err = firewall.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create firewall module: %w", err)
	}
	instance.filterLists, err = filterlists.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create filterLists module: %w", err)
	}
	instance.interception, err = interception.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create interception module: %w", err)
	}
	instance.dnsmonitor, err = dnsmonitor.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create dns-listener module: %w", err)
	}
	instance.customlist, err = customlists.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create customlist module: %w", err)
	}
	instance.status, err = status.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create status module: %w", err)
	}
	instance.broadcasts, err = broadcasts.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create broadcasts module: %w", err)
	}
	instance.compat, err = compat.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create compat module: %w", err)
	}
	instance.nameserver, err = nameserver.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create nameserver module: %w", err)
	}
	instance.process, err = process.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create process module: %w", err)
	}
	instance.resolver, err = resolver.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create resolver module: %w", err)
	}
	instance.sync, err = sync.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create sync module: %w", err)
	}
	instance.access, err = access.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create access module: %w", err)
	}

	// SPN modules
	instance.cabin, err = cabin.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create cabin module: %w", err)
	}
	instance.navigator, err = navigator.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create navigator module: %w", err)
	}
	instance.captain, err = captain.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create captain module: %w", err)
	}
	instance.crew, err = crew.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create crew module: %w", err)
	}
	instance.docks, err = docks.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create docks module: %w", err)
	}
	instance.patrol, err = patrol.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create patrol module: %w", err)
	}
	instance.ships, err = ships.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create ships module: %w", err)
	}
	instance.sluice, err = sluice.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create sluice module: %w", err)
	}
	instance.terminal, err = terminal.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create terminal module: %w", err)
	}

	// Add all modules to instance group.
	instance.serviceGroup = mgr.NewGroup(
		instance.base,
		instance.rng,
		instance.database,
		instance.config,
		instance.api,
		instance.metrics,
		instance.runtime,
		instance.notifications,

		instance.core,
		instance.updates,
		instance.integration,
		instance.geoip,
		instance.netenv,

		instance.process,
		instance.profile,
		instance.network,
		instance.netquery,
		instance.firewall,
		instance.nameserver,
		instance.resolver,
		instance.filterLists,
		instance.customlist,
		instance.interception,
		instance.dnsmonitor,

		instance.compat,
		instance.status,
		instance.broadcasts,
		instance.sync,
		instance.ui,

		instance.access,
	)

	// SPN Group
	instance.SpnGroup = mgr.NewExtendedGroup(
		instance.cabin,
		instance.navigator,
		instance.captain,
		instance.crew,
		instance.docks,
		instance.patrol,
		instance.ships,
		instance.sluice,
		instance.terminal,
	)

	return instance, nil
}

// SleepyModule is an interface for modules that can enter some sort of sleep mode.
type SleepyModule interface {
	SetSleep(enabled bool)
}

// SetSleep sets sleep mode on all modules that satisfy the SleepyModule interface.
func (i *Instance) SetSleep(enabled bool) {
	for _, module := range i.serviceGroup.Modules() {
		if sm, ok := module.(SleepyModule); ok {
			sm.SetSleep(enabled)
		}
	}
	for _, module := range i.SpnGroup.Modules() {
		if sm, ok := module.(SleepyModule); ok {
			sm.SetSleep(enabled)
		}
	}
}

// Database returns the database module.
func (i *Instance) Database() *dbmodule.DBModule {
	return i.database
}

// Config returns the config module.
func (i *Instance) Config() *config.Config {
	return i.config
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

// Base returns the base module.
func (i *Instance) Base() *base.Base {
	return i.base
}

// Updates returns the updates module.
func (i *Instance) Updates() *updates.Updates {
	return i.updates
}

// OSIntegration returns the integration module.
func (i *Instance) OSIntegration() *integration.OSIntegration {
	return i.integration
}

// GeoIP returns the geoip module.
func (i *Instance) GeoIP() *geoip.GeoIP {
	return i.geoip
}

// NetEnv returns the netenv module.
func (i *Instance) NetEnv() *netenv.NetEnv {
	return i.netenv
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

// Docks returns the crew module.
func (i *Instance) Docks() *docks.Docks {
	return i.docks
}

// Navigator returns the navigator module.
func (i *Instance) Navigator() *navigator.Navigator {
	return i.navigator
}

// Patrol returns the patrol module.
func (i *Instance) Patrol() *patrol.Patrol {
	return i.patrol
}

// Ships returns the ships module.
func (i *Instance) Ships() *ships.Ships {
	return i.ships
}

// Sluice returns the ships module.
func (i *Instance) Sluice() *sluice.SluiceModule {
	return i.sluice
}

// Terminal returns the terminal module.
func (i *Instance) Terminal() *terminal.TerminalModule {
	return i.terminal
}

// UI returns the ui module.
func (i *Instance) UI() *ui.UI {
	return i.ui
}

// Profile returns the profile module.
func (i *Instance) Profile() *profile.ProfileModule {
	return i.profile
}

// Firewall returns the firewall module.
func (i *Instance) Firewall() *firewall.Firewall {
	return i.firewall
}

// FilterLists returns the filterLists module.
func (i *Instance) FilterLists() *filterlists.FilterLists {
	return i.filterLists
}

// Interception returns the interception module.
func (i *Instance) Interception() *interception.Interception {
	return i.interception
}

// DNSMonitor returns the dns-listener module.
func (i *Instance) DNSMonitor() *dnsmonitor.DNSMonitor {
	return i.dnsmonitor
}

// CustomList returns the customlist module.
func (i *Instance) CustomList() *customlists.CustomList {
	return i.customlist
}

// Status returns the status module.
func (i *Instance) Status() *status.Status {
	return i.status
}

// Broadcasts returns the broadcast module.
func (i *Instance) Broadcasts() *broadcasts.Broadcasts {
	return i.broadcasts
}

// Compat returns the compat module.
func (i *Instance) Compat() *compat.Compat {
	return i.compat
}

// NameServer returns the nameserver module.
func (i *Instance) NameServer() *nameserver.NameServer {
	return i.nameserver
}

// NetQuery returns the netquery module.
func (i *Instance) NetQuery() *netquery.NetQuery {
	return i.netquery
}

// Network returns the network module.
func (i *Instance) Network() *network.Network {
	return i.network
}

// Process returns the process module.
func (i *Instance) Process() *process.ProcessModule {
	return i.process
}

// Resolver returns the resolver module.
func (i *Instance) Resolver() *resolver.ResolverModule {
	return i.resolver
}

// Sync returns the sync module.
func (i *Instance) Sync() *sync.Sync {
	return i.sync
}

// Core returns the core module.
func (i *Instance) Core() *core.Core {
	return i.core
}

// SPNGroup returns the group of all SPN modules.
func (i *Instance) SPNGroup() *mgr.ExtendedGroup {
	return i.SpnGroup
}

// Events

// GetEventSPNConnected return the event manager for the SPN connected event.
func (i *Instance) GetEventSPNConnected() *mgr.EventMgr[struct{}] {
	return i.captain.EventSPNConnected
}

// Special functions

// SetCmdLineOperation sets a command line operation to be executed instead of starting the system. This is useful when functions need all modules to be prepared for a special operation.
func (i *Instance) SetCmdLineOperation(f func() error) {
	i.CommandLineOperation = f
}

// GetStates returns the current states of all group modules.
func (i *Instance) GetStates() []mgr.StateUpdate {
	mainStates := i.serviceGroup.GetStates()
	spnStates := i.SpnGroup.GetStates()

	updates := make([]mgr.StateUpdate, 0, len(mainStates)+len(spnStates))
	updates = append(updates, mainStates...)
	updates = append(updates, spnStates...)

	return updates
}

// AddStatesCallback adds the given callback function to all group modules that
// expose a state manager at States().
func (i *Instance) AddStatesCallback(callbackName string, callback mgr.EventCallbackFunc[mgr.StateUpdate]) {
	i.serviceGroup.AddStatesCallback(callbackName, callback)
	i.SpnGroup.AddStatesCallback(callbackName, callback)
}

// Ready returns whether all modules in the main service module group have been started and are still running.
func (i *Instance) Ready() bool {
	return i.serviceGroup.Ready()
}

// Ctx returns the instance context.
// It is only canceled on shutdown.
func (i *Instance) Ctx() context.Context {
	return i.ctx
}

// Start starts the instance.
func (i *Instance) Start() error {
	return i.serviceGroup.Start()
}

// Stop stops the instance and cancels the instance context when done.
func (i *Instance) Stop() error {
	defer i.cancelCtx()
	return i.serviceGroup.Stop()
}

// RestartExitCode will instruct portmaster-start to restart the process immediately, potentially with a new version.
const RestartExitCode = 23

// Restart asynchronously restarts the instance.
// This only works if the underlying system/process supports this.
func (i *Instance) Restart() {
	// Send a restart event, give it 10ms extra to propagate.
	i.core.EventRestart.Submit(struct{}{})
	time.Sleep(10 * time.Millisecond)

	i.shutdown(RestartExitCode)
}

// Shutdown asynchronously stops the instance.
func (i *Instance) Shutdown() {
	// Send a shutdown event, give it 10ms extra to propagate.
	i.core.EventShutdown.Submit(struct{}{})
	time.Sleep(10 * time.Millisecond)

	i.shutdown(0)
}

func (i *Instance) shutdown(exitCode int) {
	// Set given exit code.
	i.exitCode.Store(int32(exitCode))

	m := mgr.New("instance")
	m.Go("shutdown", func(w *mgr.WorkerCtx) error {
		for {
			if err := i.Stop(); err != nil {
				w.Error("failed to shutdown", "err", err, "retry", "1s")
				time.Sleep(1 * time.Second)
			} else {
				return nil
			}
		}
	})
}

// Stopping returns whether the instance is shutting down.
func (i *Instance) Stopping() bool {
	return i.ctx.Err() != nil
}

// Stopped returns a channel that is triggered when the instance has shut down.
func (i *Instance) Stopped() <-chan struct{} {
	return i.ctx.Done()
}

// ExitCode returns the set exit code of the instance.
func (i *Instance) ExitCode() int {
	return int(i.exitCode.Load())
}
