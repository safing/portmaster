package spn

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/core"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/intel/filterlists"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
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
	ctx       context.Context
	cancelCtx context.CancelFunc

	shutdownCtx       context.Context
	cancelShutdownCtx context.CancelFunc

	serviceGroup *mgr.Group

	binDir  string
	dataDir string

	exitCode atomic.Int32

	base     *base.Base
	database *dbmodule.DBModule
	config   *config.Config
	api      *api.API
	metrics  *metrics.Metrics
	runtime  *runtime.Runtime
	rng      *rng.Rng

	core          *core.Core
	binaryUpdates *updates.Updater
	intelUpdates  *updates.Updater
	geoip         *geoip.GeoIP
	netenv        *netenv.NetEnv
	filterLists   *filterlists.FilterLists

	access    *access.Access
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
	ShouldRestart        bool
}

// New returns a new Portmaster service instance.
func New(svcCfg *service.ServiceConfig) (*Instance, error) {
	// Initialize config.
	err := svcCfg.Init()
	if err != nil {
		return nil, fmt.Errorf("internal service config error: %w", err)
	}

	// Make sure data dir exists, so that child directories don't dictate the permissions.
	err = os.MkdirAll(svcCfg.DataDir, 0o0755)
	if err != nil {
		return nil, fmt.Errorf("data directory %s is not accessible: %w", svcCfg.DataDir, err)
	}

	// Create instance to pass it to modules.
	instance := &Instance{
		binDir:  svcCfg.BinDir,
		dataDir: svcCfg.DataDir,
	}
	instance.ctx, instance.cancelCtx = context.WithCancel(context.Background())
	instance.shutdownCtx, instance.cancelShutdownCtx = context.WithCancel(context.Background())

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
	instance.rng, err = rng.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create rng module: %w", err)
	}

	// Service modules
	binaryUpdateConfig, intelUpdateConfig, err := service.MakeUpdateConfigs(svcCfg)
	if err != nil {
		return instance, fmt.Errorf("create updates config: %w", err)
	}
	instance.binaryUpdates, err = updates.New(instance, "Binary Updater", *binaryUpdateConfig)
	if err != nil {
		return instance, fmt.Errorf("create updates module: %w", err)
	}
	instance.intelUpdates, err = updates.New(instance, "Intel Updater", *intelUpdateConfig)
	if err != nil {
		return instance, fmt.Errorf("create updates module: %w", err)
	}
	instance.core, err = core.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create core module: %w", err)
	}
	instance.geoip, err = geoip.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create customlist module: %w", err)
	}
	instance.netenv, err = netenv.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create netenv module: %w", err)
	}
	instance.filterLists, err = filterlists.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create filterLists module: %w", err)
	}

	// SPN modules
	instance.access, err = access.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create access module: %w", err)
	}
	instance.cabin, err = cabin.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create cabin module: %w", err)
	}
	instance.navigator, err = navigator.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create navigator module: %w", err)
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
	instance.captain, err = captain.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create captain module: %w", err)
	}

	// Add all modules to instance group.
	instance.serviceGroup = mgr.NewGroup(
		instance.base,
		instance.database,
		instance.config,
		instance.api,
		instance.metrics,
		instance.runtime,
		instance.rng,

		instance.core,
		instance.binaryUpdates,
		instance.intelUpdates,
		instance.geoip,
		instance.netenv,

		instance.access,
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

// AddModule validates the given module and adds it to the service group, if all requirements are met.
// All modules must be added before anything is done with the instance.
func (i *Instance) AddModule(m mgr.Module) {
	i.serviceGroup.Add(m)
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
}

// BinDir returns the directory for binaries.
// This directory may be read-only.
func (i *Instance) BinDir() string {
	return i.binDir
}

// DataDir returns the directory for variable data.
// This directory is expected to be read/writeable.
func (i *Instance) DataDir() string {
	return i.dataDir
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

// Rng returns the rng module.
func (i *Instance) Rng() *rng.Rng {
	return i.rng
}

// Base returns the base module.
func (i *Instance) Base() *base.Base {
	return i.base
}

// BinaryUpdates returns the updates module.
func (i *Instance) BinaryUpdates() *updates.Updater {
	return i.binaryUpdates
}

// IntelUpdates returns the updates module.
func (i *Instance) IntelUpdates() *updates.Updater {
	return i.intelUpdates
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

// FilterLists returns the filterLists module.
func (i *Instance) FilterLists() *filterlists.FilterLists {
	return i.filterLists
}

// Core returns the core module.
func (i *Instance) Core() *core.Core {
	return i.core
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
	return i.serviceGroup.GetStates()
}

// AddStatesCallback adds the given callback function to all group modules that
// expose a state manager at States().
func (i *Instance) AddStatesCallback(callbackName string, callback mgr.EventCallbackFunc[mgr.StateUpdate]) {
	i.serviceGroup.AddStatesCallback(callbackName, callback)
}

// Ready returns whether all modules in the main service module group have been started and are still running.
func (i *Instance) Ready() bool {
	return i.serviceGroup.Ready()
}

// Start starts the instance.
func (i *Instance) Start() error {
	return i.serviceGroup.Start()
}

// Stop stops the instance and cancels the instance context when done.
func (i *Instance) Stop() error {
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

	// Set the restart flag and shutdown.
	i.ShouldRestart = true
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
	// Only shutdown once.
	if i.IsShuttingDown() {
		return
	}

	// Cancel main  context.
	i.cancelCtx()

	// Set given exit code.
	i.exitCode.Store(int32(exitCode))

	// Start shutdown asynchronously in a separate manager.
	m := mgr.New("instance")
	m.Go("shutdown", func(w *mgr.WorkerCtx) error {
		// Stop all modules.
		if err := i.Stop(); err != nil {
			w.Error("failed to shutdown", "err", err)
		}

		// Cancel shutdown process context.
		i.cancelShutdownCtx()
		return nil
	})
}

// Ctx returns the instance context.
// It is canceled when shutdown is started.
func (i *Instance) Ctx() context.Context {
	return i.ctx
}

// IsShuttingDown returns whether the instance is shutting down.
func (i *Instance) IsShuttingDown() bool {
	return i.ctx.Err() != nil
}

// ShuttingDown returns a channel that is triggered when the instance starts shutting down.
func (i *Instance) ShuttingDown() <-chan struct{} {
	return i.ctx.Done()
}

// ShutdownCtx returns the instance shutdown context.
// It is canceled when shutdown is complete.
func (i *Instance) ShutdownCtx() context.Context {
	return i.shutdownCtx
}

// IsShutDown returns whether the instance has stopped.
func (i *Instance) IsShutDown() bool {
	return i.shutdownCtx.Err() != nil
}

// ShutDownComplete returns a channel that is triggered when the instance has shut down.
func (i *Instance) ShutdownComplete() <-chan struct{} {
	return i.shutdownCtx.Done()
}

// ExitCode returns the set exit code of the instance.
func (i *Instance) ExitCode() int {
	return int(i.exitCode.Load())
}

// SPNGroup fakes interface conformance.
// SPNGroup is only needed on SPN clients.
func (i *Instance) SPNGroup() *mgr.ExtendedGroup {
	return nil
}

// Unsupported Modules.

// Notifications returns nil.
func (i *Instance) Notifications() *notifications.Notifications { return nil }
