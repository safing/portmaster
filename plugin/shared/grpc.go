package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type GRPCClient struct {
	broker *plugin.GRPCBroker
	client proto.DeciderServiceClient
}

func (m *GRPCClient) DecideOnConnection() error {

	res, err := m.client.DecideOnConnection(context.Background(), &proto.DecideOnConnectionRequest{})
	if err != nil {
		return err
	}

	_ = res

	return nil
}

type GRPCServer struct {
	proto.UnimplementedDeciderServiceServer

	Impl   Decider
	broker *plugin.GRPCBroker
}

func (m *GRPCServer) DecideOnConnection(ctx context.Context, req *proto.DecideOnConnectionRequest) (*proto.DecideOnConnectionResponse, error) {
	return new(proto.DecideOnConnectionResponse), nil
}
