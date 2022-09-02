package base

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/config"
	"github.com/safing/portmaster/plugin/shared/notification"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type (
	gRPCClient struct {
		broker *plugin.GRPCBroker
		client proto.BaseServiceClient
	}

	gRPCServer struct {
		proto.UnimplementedBaseServiceServer

		Impl   Base
		broker *plugin.GRPCBroker
	}
)

func (m *gRPCClient) Configure(ctx context.Context, env *proto.ConfigureRequest, cfg config.Service, notif notification.Service) error {
	var s *grpc.Server
	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		s = grpc.NewServer(opts...)
		proto.RegisterConfigServiceServer(s, &config.GRPCServer{
			PluginName: env.PluginName,
			Impl:       cfg,
		})

		proto.RegisterNotificationServiceServer(s, &notification.GRPCServer{
			PluginName: env.PluginName,
			Impl:       notif,
		})

		return s
	}

	brokerID := m.broker.NextId()
	go m.broker.AcceptAndServe(brokerID, serverFunc)

	env.BackchannelId = brokerID

	res, err := m.client.Configure(ctx, env)
	if err != nil {
		return err
	}

	_ = res

	return nil
}

func (m *gRPCClient) Shutdown(ctx context.Context) error {
	_, err := m.client.Shutdown(ctx, &proto.ShutdownRequest{})

	return err
}

func (m *gRPCServer) Configure(ctx context.Context, req *proto.ConfigureRequest) (*proto.ConfigureResponse, error) {
	conn, err := m.broker.Dial(req.BackchannelId)
	if err != nil {
		return nil, err
	}

	configClient := &config.GRPCClient{
		Client: proto.NewConfigServiceClient(conn),
	}

	notifClient := &notification.GRPCClient{
		Client: proto.NewNotificationServiceClient(conn),
	}

	if err := m.Impl.Configure(ctx, req, configClient, notifClient); err != nil {
		return nil, err
	}

	return new(proto.ConfigureResponse), nil
}

func (m *gRPCServer) Shutdown(ctx context.Context, _ *proto.ShutdownRequest) (*proto.ShutdownResponse, error) {
	err := m.Impl.Shutdown(ctx)
	if err != nil {
		return nil, err
	}

	return &proto.ShutdownResponse{}, nil
}

var _ plugin.GRPCPlugin = new(Plugin)
