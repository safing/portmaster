package loader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	plugin "github.com/hashicorp/go-plugin"
	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/proto"
)

// PluginLoader is capable of loading Portmaster plugins for a specified set
// of search directories.
type PluginLoader struct {
	SearchDirectories []string

	module *modules.Module

	l             sync.Mutex
	loadedPlugins map[string]*plugin.Client
}

// NewPluginLoader returns a new PluginLoader instance that looks for plugins
// in path.
func NewPluginLoader(m *modules.Module, baseDir ...string) *PluginLoader {
	return &PluginLoader{
		module:            m,
		SearchDirectories: baseDir,
		loadedPlugins:     make(map[string]*plugin.Client),
	}
}

var (
	ErrPluginNotFound = errors.New("plugin not found in search paths")
)

func (ldr *PluginLoader) Dispense(ctx context.Context, pluginName string, pluginType shared.PluginType) (interface{}, error) {
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

		client = plugin.NewClient(&plugin.ClientConfig{
			HandshakeConfig: shared.Handshake,
			Plugins:         shared.PluginMap,
			Cmd:             exec.Command(path),
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
			return nil, fmt.Errorf("plugin %s does not implement base plugin: %w", pluginName, err)
		}

		// configure the plugin now, if that fails kill it and return the error.
		env := &proto.ConfigureRequest{
			BaseDirectory: dataroot.Root().Path,
			PluginName:    pluginName,
		}

		// create a configuration server that just accepts configuration
		// requests from this plugin.
		// The server will ensure plugins have limited permission to create
		// and read keys under the "config:plugins/<plugin-name>" scope.
		cfg := shared.NewHostConfigServer(ldr.module, pluginName)

		base := pluginImpl.(shared.Base)
		// TODO(ppacher): make sure we don't need to pass in nil here for
		// shared.Config.
		// It's actually already handled by the GRPCBaseClient
		if err := base.Configure(ctx, env, cfg); err != nil {
			client.Kill()

			return nil, fmt.Errorf("failed to configure plugin: %w", err)
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

func (ldr *PluginLoader) Kill() {
	ldr.l.Lock()
	defer ldr.l.Unlock()

	for _, client := range ldr.loadedPlugins {
		if client.Exited() {
			continue
		}

		client.Kill()
	}
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
