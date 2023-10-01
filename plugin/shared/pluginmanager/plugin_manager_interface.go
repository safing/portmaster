package pluginmanager

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type Service interface {
	// ListPlugins returns a list of all registered plugins.
	ListPlugins(ctx context.Context) ([]*proto.Plugin, error)

	// RegisterPlugin registers a new plugin configuration at the plugin
	// loader. Note that this is a runtime change only and does not update
	// the plugin JSON configuration file.
	RegisterPlugin(ctx context.Context, req *proto.PluginConfig) error

	// UnregisterPlugin removes a plugin registration from the loader.
	// If the plugin is currently running it will be stopped before
	// being removed.
	// Note that this is a runtime change only and does not update
	// the plugin JSON configuration file.
	UnregisterPlugin(ctx context.Context, name string) error

	// StopPlugin stops a running pluging.
	StopPlugin(ctx context.Context, name string) error

	// StartPlugin starts a plugin. Note that the plugin configuration
	// must have been registered before using RegisterPlugin.
	StartPlugin(ctx context.Context, name string) error
}
