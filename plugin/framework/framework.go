package framework

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/proto"
)

var (
	ErrPluginTypeRegistered = errors.New("plugin type already registered")
)

type (
	Plugin struct {
		base      basePlugin
		pluginMap plugin.PluginSet
	}

	DeciderFunc func(context.Context, *proto.Connection) (proto.Verdict, string, error)

	ReporterFunc func(context.Context, *proto.Connection) error
)

func (fn DeciderFunc) DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error) {
	return fn(ctx, conn)
}

func (fn ReporterFunc) ReportConnection(ctx context.Context, conn *proto.Connection) error {
	return fn(ctx, conn)
}

func (plg *Plugin) RegisterReporter(r shared.Reporter) error {
	if _, ok := plg.pluginMap["reporter"]; ok {
		return fmt.Errorf("reporter: %w", ErrPluginTypeRegistered)
	}

	if plg.pluginMap == nil {
		plg.pluginMap = plugin.PluginSet{}
	}

	plg.pluginMap["reporter"] = &shared.ReporterPlugin{
		Impl: r,
	}

	return nil
}

func (plg *Plugin) RegisterDecider(r shared.Decider) error {
	if _, ok := plg.pluginMap["decider"]; ok {
		return fmt.Errorf("decider: %w", ErrPluginTypeRegistered)
	}

	if plg.pluginMap == nil {
		plg.pluginMap = plugin.PluginSet{}
	}

	plg.pluginMap["decider"] = &shared.DeciderPlugin{
		Impl: r,
	}

	return nil
}

func (plg *Plugin) Serve() {
	pluginSet := plg.pluginMap

	if pluginSet == nil {
		pluginSet = plugin.PluginSet{}
	}

	pluginSet["base"] = &shared.BasePlugin{
		Impl: &plg.base,
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: shared.Handshake,
		GRPCServer:      plugin.DefaultGRPCServer,
		Plugins:         pluginSet,
	})
}

var Default = new(Plugin)

func RegisterDecider(d shared.Decider) error {
	return Default.RegisterDecider(d)
}

func RegisterReporter(r shared.Reporter) error {
	return Default.RegisterReporter(r)
}

func BaseDirectory() string {
	return Default.base.BaseDirectory()
}

func PluginName() string {
	return Default.base.PluginName()
}

func Serve() {
	Default.Serve()
}

func OnInit(fn func() error) {
	Default.base.OnInit(fn)
}

func Config() shared.Config {
	return Default.base.Config
}

var (
	_ shared.Decider  = new(DeciderFunc)
	_ shared.Reporter = new(ReporterFunc)
)
