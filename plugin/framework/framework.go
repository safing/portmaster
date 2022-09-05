package framework

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/base"
	"github.com/safing/portmaster/plugin/shared/config"
	"github.com/safing/portmaster/plugin/shared/decider"
	"github.com/safing/portmaster/plugin/shared/notification"
	"github.com/safing/portmaster/plugin/shared/pluginmanager"
	"github.com/safing/portmaster/plugin/shared/reporter"
)

var (
	// ErrPluginTypeRegistered is returned when trying to register a
	// plugin type multiple times like when calling AddReporter more than
	// once.
	ErrPluginTypeRegistered = errors.New("plugin type already registered")

	// ErrNoStaticConfig is returned by ParseStaticConfig when no static
	// configuration has been provided in plugins.json.
	ErrNoStaticConfig = errors.New("no static configuration available")
)

// Plugin is implements utility methods for registering plugin types
// and actually serving the plugin when launched by the plugin host (Portmaster).
// It also implements the base.Base plugin types so user do not need to
// care about that plugin type and can use provided utility methods
// to access the Portmaster configuration and notification system as
// well as additional plugin environment.
type Plugin struct {
	BasePlugin
	pluginMap plugin.PluginSet
}

// RegisterReporter registers a reporter plugin type.
// The provided reporter will only be used if the Plugin Host (Portmaster)
// is instructed to use the "reporter" plugin type when the plugin is dispensed.
//
// This may only be called once and is not save to be called concurrently.
func (plg *Plugin) RegisterReporter(r reporter.Reporter) error {
	if _, ok := plg.pluginMap["reporter"]; ok {
		return fmt.Errorf("reporter: %w", ErrPluginTypeRegistered)
	}

	if plg.pluginMap == nil {
		plg.pluginMap = plugin.PluginSet{}
	}

	plg.pluginMap["reporter"] = &reporter.Plugin{
		Impl: r,
	}

	return nil
}

// RegisterDecider registers a decider plugin type.
// The provded decider will only be used if the Plugin Host (Portmaster)
// is instructed to use the "decider" plugin type when the plugin is dispensed.
//
// This may only be called once and is not safe to be called concurrently.
// If you want to register multiple deciders use ChainDecider or ChainDeciderFunc
// to create a combinded decider implementation.
func (plg *Plugin) RegisterDecider(r decider.Decider) error {
	if _, ok := plg.pluginMap["decider"]; ok {
		return fmt.Errorf("decider: %w", ErrPluginTypeRegistered)
	}

	if plg.pluginMap == nil {
		plg.pluginMap = plugin.PluginSet{}
	}

	plg.pluginMap["decider"] = &decider.Plugin{
		Impl: r,
	}

	return nil
}

// Serve starts serving the plugin. It should be called last in the
// plugin main() and will block until the plugin is killed by the
// plugin host (Portmaster).
//
// Users should make sure to register their implemented plugin types
// using RegisterDecider, RegisterReporter or similar before calling
// Serve.
func (plg *Plugin) Serve() {
	pluginSet := plg.pluginMap

	if pluginSet == nil {
		pluginSet = plugin.PluginSet{}
	}

	plg.baseCtx, plg.cancel = context.WithCancel(context.Background())

	pluginSet["base"] = &base.Plugin{
		Impl: &plg.BasePlugin,
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.Handshake,
		GRPCServer:      plugin.DefaultGRPCServer,
		Plugins:         pluginSet,
	})
}

// Default is the default instance of Plugin so users can
// use package-level functions for ease-of-use.
var Default = new(Plugin)

// RegisterDecider calls through to Plugin.RegisterDecider using
// the Default plugin instance.
func RegisterDecider(d decider.Decider) error {
	return Default.RegisterDecider(d)
}

// RegisterReporter calls through to Plugin.RegisterReporter using
// the Default plugin instance.
func RegisterReporter(r reporter.Reporter) error {
	return Default.RegisterReporter(r)
}

// BaseDirectory returns the base directory of the portmaster installation.
// It basically calls through to BasePlugin.BaseDirectory of the Default
// plugin instance.
func BaseDirectory() string {
	return Default.BaseDirectory()
}

// PluginName returns the name of the plugin as specified by the user.
//
// It basically calls through to BasePlugin.PluginName of the Default
// plugin instance.
func PluginName() string {
	return Default.PluginName()
}

// ParseStaticConfig parses the static plugin configuration into receiver.
//
// It basically calls through to BasePlugin.ParseStaticConfig of the Default
// plugin instance.
func ParseStaticConfig(receiver interface{}) error {
	return Default.ParseStaticConfig(receiver)
}

// Serve serves the plugin.
//
// It basically calls through to Plugin.Serve of the Default
// plugin instance.
func Serve() {
	Default.Serve()
}

// OnInit registers a new on-init function to be called when the plugin is
// dispensed and configured.
//
// It basically calls through to BasePlugin.OnInit of the Default
// plugin instance.
func OnInit(fn func(context.Context) error) {
	Default.OnInit(fn)
}

// OnShutdown registers a new on-shutdown function to be called when the
// plugin is requested to shut-down
//
// It basically calls through to BasePlugin.OnShutdown of the Default
// plugin instance.
func OnShutdown(fn func(context.Context) error) {
	Default.OnShutdown(fn)
}

// Config returns access to the Portmaster configuration system.
//
// It's basically the same as accessing the Config of the Default
// plugin instance.
func Config() config.Service {
	return Default.Environment.Config
}

// Notify returns access to the Portmaster notification system.
//
// It's basically the same as accessing the Notification of the Default
// plugin instance.
func Notify() notification.Service {
	return Default.Environment.Notify
}

// PluginManager returns access to the Portmaster plugin-management system.
// Note that the PluginManager is only available when the plugin has been configured
// as privileged.
//
// It's basically the same as accessing the PluginManager of the Default
// plugin instance.
func PluginManager() pluginmanager.Service {
	return Default.Environment.PluginManager
}

// PortmasterVersion returns the current version of the Portmaster that launched
// the plugin instance.
//
// It's basically the same as accessing the PortmasterVersion of the Default
// plugin instance.
func PortmasterVersion() string {
	return Default.PortmasterVersion
}

// Context returns the default context of the plugin and is cancelled as
// soon as a plugin shutdown request is received.
func Context() context.Context {
	return Default.Context()
}
