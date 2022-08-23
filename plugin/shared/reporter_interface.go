package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type Reporter interface {
	ReportConnection(ctx context.Context, conn *proto.Connection) error
}

type ReporterPlugin struct {
	plugin.NetRPCUnsupportedPlugin

	Impl Reporter
}

func (p *ReporterPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterReporterServiceServer(s, &GRPCReporterServer{
		Impl:   p.Impl,
		broker: broker,
	})

	return nil
}

func (p *ReporterPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCReporterClient{
		client: proto.NewReporterServiceClient(c),
		broker: broker,
	}, nil
}

var _ plugin.GRPCPlugin = &ReporterPlugin{}
