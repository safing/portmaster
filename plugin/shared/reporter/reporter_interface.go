package reporter

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type (
	Reporter interface {
		ReportConnection(ctx context.Context, conn *proto.Connection) error
	}

	Plugin struct {
		plugin.NetRPCUnsupportedPlugin

		Impl Reporter
	}
)

func (p *Plugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterReporterServiceServer(s, &gRPCServer{
		Impl:   p.Impl,
		broker: broker,
	})

	return nil
}

func (p *Plugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &gRPCClient{
		client: proto.NewReporterServiceClient(c),
		broker: broker,
	}, nil
}

var _ plugin.GRPCPlugin = &Plugin{}
