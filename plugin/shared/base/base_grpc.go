package base

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/config"
	"github.com/safing/portmaster/plugin/shared/notification"
	"github.com/safing/portmaster/plugin/shared/pluginmanager"
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

func (m *gRPCClient) Configure(ctx context.Context, req *proto.ConfigureRequest, env Environment) error {
	var s *grpc.Server
	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		s = grpc.NewServer(opts...)

		if env.Config != nil {
			proto.RegisterConfigServiceServer(s, &config.GRPCServer{
				PluginName: req.GetConfig().GetName(),
				Impl:       env.Config,
			})
		}

		if env.Notify != nil {
			proto.RegisterNotificationServiceServer(s, &notification.GRPCServer{
				PluginName: req.GetConfig().GetName(),
				Impl:       env.Notify,
			})
		}

		if env.PluginManager != nil {
			proto.RegisterPluginManagerServiceServer(s, &pluginmanager.GRPCServer{
				Impl: env.PluginManager,
			})
		}

		return s
	}

	brokerID := m.broker.NextId()
	go m.broker.AcceptAndServe(brokerID, serverFunc)

	req.BackchannelId = brokerID

	res, err := m.client.Configure(ctx, req)
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

	notifyClient := &notification.GRPCClient{
		Client: proto.NewNotificationServiceClient(conn),
	}

	var pluginManagerClient pluginmanager.Service

	// access to the pluginmanager is only allowed for privileged plugins.
	// since the pluginmanager GRPC server will not be started for unprivileged plugins
	// there's now reason to try to create a client for that.
	// Furthermore, users may just check if framework.PluginManager() returns
	// nil to know if the plugin has been configured as privileged or not.
	if req.GetConfig().GetPrivileged() {
		pluginManagerClient = &pluginmanager.GRPCClient{
			Client: proto.NewPluginManagerServiceClient(conn),
		}
	}

	if err := m.Impl.Configure(ctx, req, Environment{
		Config:        configClient,
		Notify:        notifyClient,
		PluginManager: pluginManagerClient,
	}); err != nil {
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
