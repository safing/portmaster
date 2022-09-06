package loader

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	plugin "github.com/hashicorp/go-plugin"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/plugin/internal"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/base"
	"github.com/safing/portmaster/plugin/shared/decider"
	"github.com/safing/portmaster/plugin/shared/reporter"
	"github.com/safing/portmaster/plugin/shared/resolver"
)

type (
	PluginNotifyFunc func(instance *PluginInstance)

	// PluginLoader is capable of loading Portmaster plugins for a specified set
	// of search directories.
	PluginLoader struct {
		SearchDirectories []string
		pluginConfigs     []shared.PluginConfig

		module *modules.Module
		fanout *internal.EventFanout

		l               sync.Mutex
		loadedPlugins   map[string]*PluginInstance
		onPluginStarted []PluginNotifyFunc
		onPluginStopped []PluginNotifyFunc
	}
)

// Common errors returned by the PluginLoader.
var (
	ErrPluginNotFound           = errors.New("plugin not found in search paths")
	ErrPluginTypeNotImplemented = errors.New("plugin does not implement the requested type")
	ErrPluginConfigFailed       = errors.New("plugin failed to be configured")
	ErrPluginNotRegistered      = errors.New("plugin not registered")
	ErrPluginNotStarted         = errors.New("plugin has not been started")
)

// NewPluginLoader returns a new PluginLoader instance that looks for plugins
// in path.
func NewPluginLoader(m *modules.Module, baseDirs []string, pluginConfigs []shared.PluginConfig) *PluginLoader {
	return &PluginLoader{
		module:            m,
		SearchDirectories: baseDirs,
		pluginConfigs:     pluginConfigs,
		fanout:            internal.NewEventFanout(m),
		loadedPlugins:     make(map[string]*PluginInstance),
	}
}

// OnPluginStarted registers a new callback function that is invoked whenever a plugin
// has been started successfully.
func (ldr *PluginLoader) OnPluginStarted(fn PluginNotifyFunc) {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	ldr.onPluginStarted = append(ldr.onPluginStarted, fn)
}

// OnPluginShutdown registers a new callback function that is invoked whenever a plugin
// instance has been stopped.
func (ldr *PluginLoader) OnPluginShutdown(fn PluginNotifyFunc) {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	ldr.onPluginStopped = append(ldr.onPluginStopped, fn)
}

func (ldr *PluginLoader) Dispense(ctx context.Context, name string) (*PluginInstance, error) {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	cfg, ok := ldr.getPluginConfig(name)
	if !ok {
		return nil, ErrPluginNotRegistered
	}

	client, err := ldr.dispenseClient(ctx, cfg.Name)
	if err != nil {
		return nil, err
	}

	// TODO(ppacher): store the client.ReattachConfig somewhere on the disk so we can
	// reattach to the plugin instance after the Portmaster restarted.
	// for now, we just kill the pluign and re-launch it afterwards but this might not
	// be the desired behavior for all plugin types.

	// create a configuration server that just accepts configuration
	// requests from this plugin.
	// The server will ensure plugins have limited permission to create
	// and read keys under the "config:plugins/<plugin-name>" scope if they are not
	// marked as privileged by the user.
	configService := internal.NewHostConfigServer(ldr.fanout, cfg.Name, cfg.Privileged)

	// create a notification server that just accepts notification requests
	// from this plugin.
	notifyService := internal.NewHostNotificationServer(cfg.Name)

	var pluginManager *HostPluginServer

	if cfg.Privileged {
		pluginManager = NewHostPluginServer(ldr)
	}

	instance, err := NewPluginInstance(ctx, cfg, ldr, client, base.Environment{
		Config:        configService,
		Notify:        notifyService,
		PluginManager: pluginManager,
	})
	if err != nil {
		client.Kill()

		return nil, err
	}

	ldr.loadedPlugins[instance.Name] = instance

	// call out to all registered callback functions
	for _, fn := range ldr.onPluginStarted {
		fn(instance)
	}

	return instance, nil
}

func (ldr *PluginLoader) getPluginConfig(name string) (shared.PluginConfig, bool) {
	for _, cfg := range ldr.pluginConfigs {
		if cfg.Name == name {
			return cfg, true
		}
	}

	return shared.PluginConfig{}, false
}

// PluginInstances returns a slice of all currently dispensed plugin instances.
func (ldr *PluginLoader) PluginInstances() []*PluginInstance {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	res := make([]*PluginInstance, 0, len(ldr.loadedPlugins))

	for _, plg := range ldr.loadedPlugins {
		res = append(res, plg)
	}

	return res
}

// PluginConfigs returns all plugin configurations currently registered at
// the loader.
func (ldr *PluginLoader) PluginConfigs() []shared.PluginConfig {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	configs := make([]shared.PluginConfig, len(ldr.pluginConfigs))
	copy(configs, ldr.pluginConfigs)

	return configs
}

// RegisterPlugin registers a new plugin configuration at the loader but does not
// yet start it.
// Use Dispense() after a successful call to RegisterPlugin to actually dispense
// and launch the new plugin.
func (ldr *PluginLoader) RegisterPlugin(cfg shared.PluginConfig) {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	for idx, existing := range ldr.pluginConfigs {
		if existing.Name == cfg.Name {
			// replace the configuration inline
			ldr.pluginConfigs[idx] = cfg

			return
		}
	}

	ldr.pluginConfigs = append(ldr.pluginConfigs, cfg)
}

// UnregisterPlugin un-registers a plugin configuration from the loader. If the plugin
// has been started and is running it will be shutdown before the configuration is
// removed from the loader.
func (ldr *PluginLoader) UnregisterPlugin(ctx context.Context, name string) error {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	var stopError error
	plugin, ok := ldr.loadedPlugins[name]
	if ok {
		stopError = plugin.Shutdown(ctx)
	}

	for idx, existing := range ldr.pluginConfigs {
		if existing.Name == name {
			ldr.pluginConfigs = append(ldr.pluginConfigs[:idx], ldr.pluginConfigs[idx+1:]...)
			return stopError
		}
	}

	return ErrPluginNotRegistered
}

// StopInstance stops a plugin that has previously been started using Dispense.
func (ldr *PluginLoader) StopInstance(ctx context.Context, name string) error {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	plugin, ok := ldr.loadedPlugins[name]
	if !ok {
		return ErrPluginNotStarted
	}

	// when we're done here, call all registered on-stop callback
	// functions.
	// We even do that in case plugin.Shutdown() returns an error because
	// the plugin instance will be killed anyway.
	defer func() {
		for _, fn := range ldr.onPluginStopped {
			fn(plugin)
		}
	}()

	if err := plugin.Shutdown(ctx); err != nil {
		return err
	}

	return nil
}

func (ldr *PluginLoader) dispenseClient(ctx context.Context, name string) (*plugin.Client, error) {
	path, err := ldr.findPluginBinary(name)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(path)

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: shared.Handshake,
		Plugins: plugin.PluginSet{
			"base":     &base.Plugin{},
			"decider":  &decider.Plugin{},
			"reporter": &reporter.Plugin{},
			"resolver": &resolver.Plugin{},
		},
		Cmd: cmd,
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolGRPC,
		},
	})

	// try to start the plugin now. this is also called when we try to get a RPC client
	// but we want to find a start-up error early and calling start multiple times is safe
	// and won't do anything if the plugin is already started.
	if _, err := client.Start(); err != nil {
		return nil, err
	}

	return client, nil
}

// Kill kills all plugin sub-processes and resets the internal loader
// state.
// It's still possible to Dispense new plugins.
func (ldr *PluginLoader) Kill() {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	for _, client := range ldr.loadedPlugins {
		if err := client.Shutdown(context.Background()); err != nil {
			log.Errorf("plugin %s: failed to shutdown %s", client.Name, err)
		}
	}

	ldr.loadedPlugins = make(map[string]*PluginInstance)
}

func (ldr *PluginLoader) findPluginBinary(name string) (string, error) {
	if runtime.GOOS == "windows" {
		name = name + ".exe"
	}

	for _, path := range ldr.SearchDirectories {
		filePath := filepath.Join(path, name)

		stat, err := os.Stat(filePath)
		if err == nil {
			if stat.IsDir() {
				continue
			}

			return filePath, nil
		}
	}

	return "", ErrPluginNotFound
}
