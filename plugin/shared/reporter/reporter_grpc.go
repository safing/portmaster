package reporter

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	gRPCClient struct {
		broker *plugin.GRPCBroker
		client proto.ReporterServiceClient
	}

	gRPCServer struct {
		proto.UnimplementedReporterServiceServer

		Impl   Reporter
		broker *plugin.GRPCBroker
	}
)

// ReportConnection implements the gRPC client side of reporter.Service.ReportConnection.
func (m *gRPCClient) ReportConnection(ctx context.Context, conn *proto.Connection) error {
	_, err := m.client.ReportConnection(ctx, &proto.ReportConnectionRequest{
		Connection: conn,
	})
	if err != nil {
		return err
	}

	return nil
}

// ReportConnection implements the gRPC server side of reporter.Service.ReportConnection.
func (m *gRPCServer) ReportConnection(ctx context.Context, req *proto.ReportConnectionRequest) (*proto.ReportConnectionRespose, error) {
	err := m.Impl.ReportConnection(ctx, req.Connection)
	if err != nil {
		return nil, err
	}

	return new(proto.ReportConnectionRespose), nil
}
