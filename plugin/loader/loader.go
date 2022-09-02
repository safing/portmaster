package loader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	plugin "github.com/hashicorp/go-plugin"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/base"
	"github.com/safing/portmaster/plugin/shared/config"
	"github.com/safing/portmaster/plugin/shared/decider"
	"github.com/safing/portmaster/plugin/shared/notification"
	"github.com/safing/portmaster/plugin/shared/proto"
	"github.com/safing/portmaster/plugin/shared/reporter"
)

// PluginLoader is capable of loading Portmaster plugins for a specified set
// of search directories.
type PluginLoader struct {
	SearchDirectories []string

	module *modules.Module
	fanout *shared.EventFanout

	l             sync.Mutex
	loadedPlugins map[string]*plugin.Client
}

// NewPluginLoader returns a new PluginLoader instance that looks for plugins
// in path.
func NewPluginLoader(m *modules.Module, baseDir ...string) *PluginLoader {
	return &PluginLoader{
		module:            m,
		SearchDirectories: baseDir,
		fanout:            shared.NewEventFanout(m),
		loadedPlugins:     make(map[string]*plugin.Client),
	}
}

// Common errors returned by the PluginLoader.
var (
	ErrPluginNotFound           = errors.New("plugin not found in search paths")
	ErrPluginTypeNotImplemented = errors.New("plugin does not implement the requested type")
	ErrPluginConfigFailed       = errors.New("plugin failed to be configured")
)

func (ldr *PluginLoader) Dispense(ctx context.Context, pluginName string, pluginType shared.PluginType, staticConfig json.RawMessage) (any, error) {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	// check if we already loaded the plugin. If yes, we can safely request the plugin
	// type from the already running sub-process.
	client, ok := ldr.loadedPlugins[pluginName]

	// search for the plugin binary, launch it and create a new client
	if !ok {
		path, err := ldr.findPluginBinary(pluginName)
		if err != nil {
			return nil, err
		}

		cmd := exec.Command(path)

		client = plugin.NewClient(&plugin.ClientConfig{
			HandshakeConfig: shared.Handshake,
			Plugins: plugin.PluginSet{
				"base":     &base.Plugin{},
				"decider":  &decider.Plugin{},
				"reporter": &reporter.Plugin{},
			},
			Cmd: cmd,
			AllowedProtocols: []plugin.Protocol{
				plugin.ProtocolGRPC,
			},
		})

		// FIXME(ppacher): make sure we detect a failing plugin and either mark it as
		// failed or try to relaunch it.

		// TODO(ppacher): store the client.ReattachConfig somewhere on the disk so we can
		// reattach to the plugin instance after the Portmaster restarted.
		// for now, we just kill the pluign and re-launch it afterwards but this might not
		// be the desired behaviour for all plugin types.

		// request the base plugin type which must be implemented by all plugins.
		// This is required so the plugin can be initialized correctly
		rpcClient, err := client.Client()
		if err != nil {
			return nil, err
		}

		pluginImpl, err := rpcClient.Dispense(string(shared.PluginTypeBase))
		if err != nil {
			return nil, fmt.Errorf("%s: type %s: %w (%s)", pluginName, pluginType, ErrPluginTypeNotImplemented, err)
		}

		// configure the plugin now, if that fails kill it and return the error.
		env := &proto.ConfigureRequest{
			BaseDirectory: dataroot.Root().Path,
			PluginName:    pluginName,
			StaticConfig:  staticConfig,
		}

		// create a configuration server that just accepts configuration
		// requests from this plugin.
		// The server will ensure plugins have limited permission to create
		// and read keys under the "config:plugins/<plugin-name>" scope.
		cfg := config.NewHostConfigServer(ldr.fanout, pluginName)

		// create a notification server that just accepts notification requests
		// from this plugin.
		notifService := notification.NewHostNotificationServer(pluginName)

		base := pluginImpl.(base.Base)

		if err := base.Configure(ctx, env, cfg, notifService); err != nil {
			client.Kill()

			return nil, ErrPluginConfigFailed
		}

		ldr.loadedPlugins[pluginName] = client
	}

	// get a RPC client for the plugin. Subsequent calls will return the same client here.
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}

	// request an instance of the plugin type
	pluginImpl, err := rpcClient.Dispense(string(pluginType))
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to get requested plugin type: %w", err)
	}

	return pluginImpl, err
}

// Kill kills all plugin sub-processes and resets the internal loader
// state.
// It's still possible to Dispense new plugins.
func (ldr *PluginLoader) Kill() {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	for name, client := range ldr.loadedPlugins {
		if client.Exited() {
			continue
		}

		rpcClient, err := client.Client()
		if err != nil {
			log.Errorf("plugin.%s: failed to get rpc client for plugin: %s", name, err)
		} else {
			baseRaw, err := rpcClient.Dispense("base")
			if err != nil {
				log.Errorf("plugin.%s: failed to get base client: %s", name, err)
			} else {
				if err := baseRaw.(base.Base).Shutdown(ldr.module.Ctx); err != nil {
					log.Errorf("plugin.%s: failed to request plugin shutdown: %s", name, err)
				}
			}
		}

		// in any case, try to kill the plugin
		client.Kill()
	}

	ldr.loadedPlugins = make(map[string]*plugin.Client)
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
