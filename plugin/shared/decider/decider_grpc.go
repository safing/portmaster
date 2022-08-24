package decider

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	gRPCClient struct {
		broker *plugin.GRPCBroker
		client proto.DeciderServiceClient
	}

	gRPCServer struct {
		proto.UnimplementedDeciderServiceServer

		Impl   Decider
		broker *plugin.GRPCBroker
	}
)

func (m *gRPCClient) DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error) {
	res, err := m.client.DecideOnConnection(ctx, &proto.DecideOnConnectionRequest{
		Connection: conn,
	})
	if err != nil {
		return proto.Verdict_VERDICT_FAILED, "", err
	}

	return res.Verdict, res.Reason, nil
}

func (m *gRPCServer) DecideOnConnection(ctx context.Context, req *proto.DecideOnConnectionRequest) (*proto.DecideOnConnectionResponse, error) {
	res, reason, err := m.Impl.DecideOnConnection(ctx, req.Connection)
	if err != nil {
		return nil, err
	}

	return &proto.DecideOnConnectionResponse{
		Verdict: res,
		Reason:  reason,
	}, nil
}
