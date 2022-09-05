package loader

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/base"
	"github.com/safing/portmaster/plugin/shared/decider"
	"github.com/safing/portmaster/plugin/shared/proto"
	"github.com/safing/portmaster/plugin/shared/reporter"
)

var (
	ErrPluginShutdown = errors.New("plugin has been killed on request")
)

type (

	// PluginInstance wraps a running plugin and all enabled plugin types.
	// A plugin is wrapped so restarting a plugin during runtime does not
	// require users of the plugin (like the firewall package) to be informed
	// about a restarted plugin. Once a plugin-instance is created it will manage
	// the entire lifetime of the plugin and will transparently re-start and re-configure
	// the plugin process.
	PluginInstance struct {
		shared.PluginConfig

		loader *PluginLoader

		env base.Environment

		// keeps track of errors reported by the plugin, grouped by
		// the plugin type that returned the error.
		recentErrorsLock sync.Mutex
		recentErrors     map[shared.PluginType][]reportedError

		l        sync.RWMutex
		killed   bool
		client   *plugin.Client
		reporter reporter.Reporter
		decider  decider.Decider
		base     base.Base
	}

	reportedError struct {
		Time  time.Time
		Error error
	}
)

func NewPluginInstance(ctx context.Context, cfg shared.PluginConfig, loader *PluginLoader, client *plugin.Client, env base.Environment) (*PluginInstance, error) {
	instance := &PluginInstance{
		PluginConfig: cfg,
		loader:       loader,
		env:          env,
		client:       client,
		recentErrors: make(map[shared.PluginType][]reportedError),
	}

	if err := instance.dispenseRequestedTypes(); err != nil {
		return nil, err
	}

	if err := instance.configure(ctx); err != nil {
		return nil, err
	}

	return instance, nil
}

func (plg *PluginInstance) ReportedErrors() *proto.PluginErrorList {
	plg.recentErrorsLock.Lock()
	defer plg.recentErrorsLock.Unlock()

	if len(plg.recentErrors) == 0 {
		return nil
	}

	errs := &proto.PluginErrorList{}
	for pType, errors := range plg.recentErrors {
		protoErrors := make([]*proto.PluginError, len(errors))

		for idx, err := range errors {
			protoErrors[idx] = &proto.PluginError{
				Time:  uint64(err.Time.UnixNano()),
				Error: err.Error.Error(),
			}
		}

		switch pType {
		case shared.PluginTypeBase:
			errs.BaseErrors = protoErrors
		case shared.PluginTypeDecider:
			errs.DeciderErrors = protoErrors
		case shared.PluginTypeReporter:
			errs.ReporterErrors = protoErrors
		}
	}

	return errs
}

func (plg *PluginInstance) dispenseRequestedTypes() error {
	rpcClient, err := plg.client.Client()
	if err != nil {
		return fmt.Errorf("plugin %s: failed to get rpc client: %w", plg.Name, err)
	}

	baseRaw, err := rpcClient.Dispense("base")
	if err != nil {
		return fmt.Errorf("plugin %s: failed to get base plugin type: %w", plg.Name, err)
	}

	var ok bool
	plg.base, ok = baseRaw.(base.Base)

	if !ok {
		return fmt.Errorf("plugin %s: base plugin type does not correctly implement base.Base", plg.Name)
	}

	for _, pluginType := range plg.Types {
		// we always request the base plugin type so we can skip it if
		// the user decided to specify it in the type array
		if pluginType == shared.PluginTypeBase {
			continue
		}

		raw, err := rpcClient.Dispense(string(pluginType))
		if err != nil {
			return fmt.Errorf("plugin %s: failed to dispense plugin type %s: %w", plg.Name, pluginType, err)
		}

		switch pluginType {
		case shared.PluginTypeReporter:
			plg.reporter, ok = raw.(reporter.Reporter)
		case shared.PluginTypeDecider:
			plg.decider, ok = raw.(decider.Decider)
		case shared.PluginTypeResolver:
		}

		if !ok {
			return fmt.Errorf("plugin %s: plugin does not correctly implement the %s interface (%T)", plg.Name, pluginType, raw)
		}
	}

	return nil
}

func (plg *PluginInstance) reportError(pType shared.PluginType, err error) {
	plg.recentErrorsLock.Lock()
	defer plg.recentErrorsLock.Unlock()

	plg.recentErrors[pType] = append(plg.recentErrors[pType], reportedError{
		Time:  time.Now(),
		Error: err,
	})
}

func (plg *PluginInstance) configure(ctx context.Context) error {
	protoTypes, err := pluginTypesToProto(plg.Types)
	if err != nil {
		return err
	}

	req := &proto.ConfigureRequest{
		BaseDirectory: dataroot.Root().Path,
		Config: &proto.PluginConfig{
			Name:             plg.Name,
			Privileged:       plg.Privileged,
			StaticConfig:     plg.Config,
			DisableAutostart: plg.DisableAutostart,
			PluginTypes:      protoTypes,
		},
		PortmasterVersion: info.GetInfo().Version,
	}

	return plg.base.Configure(ctx, req, plg.env)
}

func (plg *PluginInstance) relaunchIfExited(ctx context.Context) error {
	if plg.killed {
		return ErrPluginShutdown
	}

	if plg.client.Exited() {
		client, err := plg.loader.dispenseClient(ctx, plg.Name)
		if err != nil {
			return fmt.Errorf("plugin %s: failed to relaunch: %w", plg.Name, err)
		}

		plg.client = client

		if err := plg.dispenseRequestedTypes(); err != nil {
			plg.reportError(shared.PluginTypeBase, err)

			return err
		}

		if err := plg.configure(ctx); err != nil {
			plg.reportError(shared.PluginTypeBase, err)

			return err
		}
	}

	return nil
}

func (plg *PluginInstance) Shutdown(ctx context.Context) error {
	plg.l.Lock()
	defer plg.l.Unlock()

	if plg.killed {
		return nil
	}

	if plg.base != nil {
		if err := plg.base.Shutdown(ctx); err != nil {
			defer plg.reportError(shared.PluginTypeBase, err)

			log.Errorf("plugin %s: failed to send shutdown request to plugin: %s", plg.Name, err)
		}

		shutdownTimeout, cancel := context.WithTimeout(ctx, time.Second*10)
		defer cancel()

	L:
		for {
			select {
			case <-time.After(time.Second):
				if plg.client.Exited() {
					break L
				}
			case <-shutdownTimeout.Done():
				plg.client.Kill()
				break L
			}
		}
	}

	plg.killed = true
	plg.reporter = nil
	plg.decider = nil
	plg.base = nil

	return nil
}

func (plg *PluginInstance) ReportConnection(ctx context.Context, conn *proto.Connection) error {
	plg.l.RLock()
	defer plg.l.RUnlock()

	if plg.reporter == nil {
		return nil
	}

	if err := plg.relaunchIfExited(ctx); err != nil {
		return err
	}

	if err := plg.reporter.ReportConnection(ctx, conn); err != nil {
		plg.reportError(shared.PluginTypeReporter, err)

		return err
	}

	return nil
}

func (plg *PluginInstance) DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error) {
	plg.l.RLock()
	defer plg.l.RUnlock()

	if plg.decider == nil {
		return proto.Verdict_VERDICT_UNDECIDED, "", nil
	}

	if err := plg.relaunchIfExited(ctx); err != nil {
		return proto.Verdict_VERDICT_FAILED, "", nil
	}

	verdict, reason, err := plg.decider.DecideOnConnection(ctx, conn)
	if err != nil {
		plg.reportError(shared.PluginTypeDecider, err)
	}

	return verdict, reason, err
}

var (
	_ reporter.Reporter = new(PluginInstance)
	_ decider.Decider   = new(PluginInstance)
)
