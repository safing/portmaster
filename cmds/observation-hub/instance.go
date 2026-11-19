package main

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/metrics"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
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

// Instance is the service instance of the observation hub.
//
// It holds only the modules an SPN client needs to watch the network, plus
// the observer and its reporting. Unlike the instances of Portmaster and of
// the hub, it has no binary updater and no core module: the observation hub
// is updated by replacing its container image, so it needs neither a binary
// directory nor a release channel. Intel data (geoip, bootstrap hubs) is still
// kept up to date by the intel updater.
type Instance struct {
	ctx       context.Context
	cancelCtx context.CancelFunc

	shutdownCtx       context.Context
	cancelShutdownCtx context.CancelFunc

	serviceGroup *mgr.Group

	dataDir string

	exitCode atomic.Int32

	base          *base.Base
	database      *dbmodule.DBModule
	config        *config.Config
	api           *api.API
	metrics       *metrics.Metrics
	runtime       *runtime.Runtime
	rng           *rng.Rng
	notifications *notifications.Notifications

	intelUpdates *updates.Updater
	geoip        *geoip.GeoIP
	netenv       *netenv.NetEnv

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

	apprise  *Apprise
	observer *Observer

	CommandLineOperation func() error
	ShouldRestart        bool
}

// NewInstance returns a new observation hub instance.
func NewInstance(svcCfg *service.ServiceConfig) (*Instance, error) {
	// Initialize config.
	err := svcCfg.Init()
	if err != nil {
		return nil, fmt.Errorf("internal service config error: %w", err)
	}

	// Make sure data dir exists, so that child directories don't dictate the permissions.
	err = utils.EnsureDirectory(svcCfg.DataDir, utils.PublicReadExecPermission)
	if err != nil {
		return nil, fmt.Errorf("data directory %s is not accessible: %w", svcCfg.DataDir, err)
	}

	// Create instance to pass it to modules.
	instance := &Instance{
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
	// The SPN client reports connection problems as notifications.
	instance.notifications, err = notifications.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create notifications module: %w", err)
	}

	// Service modules
	_, intelUpdateConfig, err := service.MakeUpdateConfigs(svcCfg)
	if err != nil {
		return instance, fmt.Errorf("create updates config: %w", err)
	}
	instance.intelUpdates, err = updates.New(instance, "Intel Updater", *intelUpdateConfig)
	if err != nil {
		return instance, fmt.Errorf("create intel updates module: %w", err)
	}
	instance.geoip, err = geoip.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create geoip module: %w", err)
	}
	instance.netenv, err = netenv.New(instance)
	if err != nil {
		return instance, fmt.Errorf("create netenv module: %w", err)
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

	// Observation hub modules. Apprise comes first, so that it is ready for the first report.
	instance.apprise, err = NewApprise(instance)
	if err != nil {
		return instance, fmt.Errorf("create apprise module: %w", err)
	}
	instance.observer, err = New(instance)
	if err != nil {
		return instance, fmt.Errorf("create observer module: %w", err)
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
		instance.notifications,

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

		instance.apprise,
		instance.observer,
	)

	return instance, nil
}

// Module access, as required by the modules.

// DataDir returns the directory for variable data.
func (i *Instance) DataDir() string { return i.dataDir }

// Config returns the config module.
func (i *Instance) Config() *config.Config { return i.config }

// IntelUpdates returns the intel updater.
func (i *Instance) IntelUpdates() *updates.Updater { return i.intelUpdates }

// NetEnv returns the netenv module.
func (i *Instance) NetEnv() *netenv.NetEnv { return i.netenv }

// Patrol returns the patrol module.
func (i *Instance) Patrol() *patrol.Patrol { return i.patrol }

// Notifications returns the notifications module.
func (i *Instance) Notifications() *notifications.Notifications { return i.notifications }

// BinaryUpdates returns nil: the observation hub has no binary updater.
// The geoip module requires the method, but does not call it.
func (i *Instance) BinaryUpdates() *updates.Updater { return nil }

// UI returns nil: the observation hub has no user interface.
func (i *Instance) UI() *ui.UI { return nil }

// SPNGroup returns nil: the SPN modules are part of the service group.
func (i *Instance) SPNGroup() *mgr.ExtendedGroup { return nil }

// Lifecycle

// Ready returns whether all modules have been started and are still running.
func (i *Instance) Ready() bool { return i.serviceGroup.Ready() }

// Start starts the instance.
func (i *Instance) Start() error { return i.serviceGroup.Start() }

// Stop stops the instance.
func (i *Instance) Stop() error { return i.serviceGroup.Stop() }

// RestartExitCode tells the supervisor to restart the process.
const RestartExitCode int32 = 23

// Restart asynchronously restarts the instance.
// This only works if the supervisor of the process restarts it.
func (i *Instance) Restart() {
	i.ShouldRestart = true
	i.shutdown(RestartExitCode)
}

// Shutdown asynchronously stops the instance.
func (i *Instance) Shutdown() {
	i.shutdown(0)
}

// Ctx returns the instance context. It is canceled when shutdown is started.
func (i *Instance) Ctx() context.Context { return i.ctx }

// IsShuttingDown returns whether the instance is shutting down.
func (i *Instance) IsShuttingDown() bool { return i.ctx.Err() != nil }

// ShuttingDown returns a channel that is closed when the instance starts shutting down.
func (i *Instance) ShuttingDown() <-chan struct{} { return i.ctx.Done() }

// ShutdownCtx returns the shutdown context. It is canceled when shutdown is complete.
func (i *Instance) ShutdownCtx() context.Context { return i.shutdownCtx }

// IsShutDown returns whether the instance has stopped.
func (i *Instance) IsShutDown() bool { return i.shutdownCtx.Err() != nil }

// ShutdownComplete returns a channel that is closed when the instance has shut down.
func (i *Instance) ShutdownComplete() <-chan struct{} { return i.shutdownCtx.Done() }

// ExitCode returns the set exit code of the instance.
func (i *Instance) ExitCode() int { return int(i.exitCode.Load()) }

// ShouldRestartIsSet returns whether the process should be restarted.
func (i *Instance) ShouldRestartIsSet() bool { return i.ShouldRestart }

// Command line operations, requested by modules during creation.

// SetCmdLineOperation sets a command line operation to be executed instead of starting the system.
func (i *Instance) SetCmdLineOperation(f func() error) { i.CommandLineOperation = f }

// CommandLineOperationIsSet returns whether a command line operation is set.
func (i *Instance) CommandLineOperationIsSet() bool { return i.CommandLineOperation != nil }

// CommandLineOperationExecute executes the set command line operation.
func (i *Instance) CommandLineOperationExecute() error { return i.CommandLineOperation() }

func (i *Instance) shutdown(exitCode int32) {
	// Only shutdown once.
	if i.IsShuttingDown() {
		return
	}

	// Cancel main context.
	i.cancelCtx()

	// Set given exit code.
	i.exitCode.Store(exitCode)

	// Start shutdown asynchronously in a separate manager.
	m := mgr.New("instance")
	m.Go("shutdown", func(w *mgr.WorkerCtx) error {
		// Stop all modules.
		err := i.Stop()
		if err != nil {
			w.Error("failed to shutdown", "err", err)
		}

		// Cancel shutdown process context.
		i.cancelShutdownCtx()
		return nil
	})
}
