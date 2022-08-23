package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type GRPCBaseClient struct {
	broker *plugin.GRPCBroker
	client proto.BaseServiceClient
}

func (m *GRPCBaseClient) Configure(ctx context.Context, env *proto.ConfigureRequest, cfg Config) error {
	var s *grpc.Server
	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		s = grpc.NewServer(opts...)
		proto.RegisterConfigServiceServer(s, &GRPCConfigServer{
			PluginName: env.PluginName,
			Impl:       cfg,
		})

		return s
	}

	brokerID := m.broker.NextId()
	go m.broker.AcceptAndServe(brokerID, serverFunc)

	env.ConfigService = brokerID

	res, err := m.client.Configure(ctx, env)
	if err != nil {
		return err
	}

	_ = res

	return nil
}

type GRPCBaseServer struct {
	proto.UnimplementedBaseServiceServer

	Impl   Base
	broker *plugin.GRPCBroker
}

func (m *GRPCBaseServer) Configure(ctx context.Context, req *proto.ConfigureRequest) (*proto.ConfigureResponse, error) {
	conn, err := m.broker.Dial(req.ConfigService)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	configClient := &GRPCConfigClient{proto.NewConfigServiceClient(conn)}

	if err := m.Impl.Configure(ctx, req, configClient); err != nil {
		return nil, err
	}

	return new(proto.ConfigureResponse), nil
}

var _ plugin.GRPCPlugin = new(BasePlugin)
