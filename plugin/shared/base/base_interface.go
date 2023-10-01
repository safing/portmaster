package base

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/config"
	"github.com/safing/portmaster/plugin/shared/notification"
	"github.com/safing/portmaster/plugin/shared/pluginmanager"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type (
	// Environment holds the plugin environment and provides access
	// to different Portmaster subsystems like the configuration and notification
	// services. If a plugin is marked as privileged, the environment will also
	// provide access to the plugin manager subsystem.
	Environment struct {
		// Config provides access to the configuration system of Portmaster.
		Config config.Service
		// Notify provides access to the notification system of Portmaster.
		Notify notification.Service
		// PluginManager provides access to the plugin system of Portmaster.
		// Note that only privileged plugins may access the plugin system
		// manager.
		PluginManager pluginmanager.Service
	}

	// Base describes base plugin requirements that are used to configure
	// the plugin and manage it's life-cycle. Plugin implementation normally don't
	// need to implement the base plugin themselvs but rather just need to use
	// the framework package which handles the more complex parts of the plugin
	// system already.
	Base interface {
		// Configure is called after the plugin has been launched and provides addition
		// information about the Portmaster host process as well as access to some
		// Portmaster sub-systems using the provided environment.
		Configure(context.Context, *proto.ConfigureRequest, Environment) error

		// Shutdown is called when the plugin should stop. Plugin implementations
		// can react to shutdown requests using framework.OnShutdown.
		Shutdown(ctx context.Context) error
	}

	// Plugin implements the plugin.GRPCPlugin interface for Base.
	Plugin struct {
		plugin.NetRPCUnsupportedPlugin

		Impl Base
	}
)

// GRPCServer registers the gRPC server side of Base and implements the plugin.GRPCPlugin interface.
func (p *Plugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterBaseServiceServer(s, &gRPCServer{
		Impl:   p.Impl,
		broker: broker,
	})

	return nil
}

// GRPCClient returns a gRPC client for Base and implements the plugin.GRPCPlugin interface.
func (p *Plugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &gRPCClient{
		client: proto.NewBaseServiceClient(c),
		broker: broker,
	}, nil
}
