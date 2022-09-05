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
	Environment struct {
		Config        config.Service
		Notify        notification.Service
		PluginManager pluginmanager.Service
	}

	Base interface {
		Configure(context.Context, *proto.ConfigureRequest, Environment) error

		Shutdown(ctx context.Context) error
	}

	Plugin struct {
		plugin.NetRPCUnsupportedPlugin

		Impl Base
	}
)

func (p *Plugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterBaseServiceServer(s, &gRPCServer{
		Impl:   p.Impl,
		broker: broker,
	})

	return nil
}

func (p *Plugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &gRPCClient{
		client: proto.NewBaseServiceClient(c),
		broker: broker,
	}, nil
}
