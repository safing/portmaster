package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type GRPCDeciderClient struct {
	broker *plugin.GRPCBroker
	client proto.DeciderServiceClient
}

func (m *GRPCDeciderClient) DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error) {
	res, err := m.client.DecideOnConnection(ctx, &proto.DecideOnConnectionRequest{
		Connection: conn,
	})
	if err != nil {
		return proto.Verdict_VERDICT_FAILED, "", err
	}

	return res.Verdict, res.Reason, nil
}

type GRPCDeciderServer struct {
	proto.UnimplementedDeciderServiceServer

	Impl   Decider
	broker *plugin.GRPCBroker
}

func (m *GRPCDeciderServer) DecideOnConnection(ctx context.Context, req *proto.DecideOnConnectionRequest) (*proto.DecideOnConnectionResponse, error) {
	res, reason, err := m.Impl.DecideOnConnection(ctx, req.Connection)
	if err != nil {
		return nil, err
	}

	return &proto.DecideOnConnectionResponse{
		Verdict: res,
		Reason:  reason,
	}, nil
}
