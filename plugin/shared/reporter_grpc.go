package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type GRPCReporterClient struct {
	broker *plugin.GRPCBroker
	client proto.ReporterServiceClient
}

func (m *GRPCReporterClient) ReportConnection(ctx context.Context, conn *proto.Connection) error {
	_, err := m.client.ReportConnection(ctx, &proto.ReportConnectionRequest{
		Connection: conn,
	})
	if err != nil {
		return err
	}

	return nil
}

type GRPCReporterServer struct {
	proto.UnimplementedReporterServiceServer

	Impl   Reporter
	broker *plugin.GRPCBroker
}

func (m *GRPCReporterServer) ReportConnection(ctx context.Context, req *proto.ReportConnectionRequest) (*proto.ReportConnectionRespose, error) {
	err := m.Impl.ReportConnection(ctx, req.Connection)
	if err != nil {
		return nil, err
	}

	return new(proto.ReportConnectionRespose), nil
}
