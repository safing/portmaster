package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type Base interface {
	Configure(context.Context, *proto.ConfigureRequest, Config) error
}

type BasePlugin struct {
	plugin.NetRPCUnsupportedPlugin

	Impl Base
}

func (p *BasePlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterBaseServiceServer(s, &GRPCBaseServer{
		Impl:   p.Impl,
		broker: broker,
	})

	return nil
}

func (p *BasePlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCBaseClient{
		client: proto.NewBaseServiceClient(c),
		broker: broker,
	}, nil
}
