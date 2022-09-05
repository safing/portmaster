package pluginmanager

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	GRPCServer struct {
		proto.UnimplementedPluginManagerServiceServer

		Impl Service
	}

	GRPCClient struct {
		Client proto.PluginManagerServiceClient
	}
)

func (srv *GRPCServer) ListPlugins(ctx context.Context, req *proto.ListPluginsRequest) (*proto.ListPluginsResponse, error) {
	plugins, err := srv.Impl.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}

	return &proto.ListPluginsResponse{
		Plugins: plugins,
	}, nil
}

func (srv *GRPCServer) RegisterPlugin(ctx context.Context, req *proto.RegisterPluginRequest) (*proto.RegisterPluginResponse, error) {
	if err := srv.Impl.RegisterPlugin(ctx, req.Config); err != nil {
		return nil, err
	}

	return &proto.RegisterPluginResponse{}, nil
}

func (srv *GRPCServer) UnregisterPlugin(ctx context.Context, req *proto.UnregisterPluginRequest) (*proto.UnregisterPluginResponse, error) {
	if err := srv.Impl.UnregisterPlugin(ctx, req.Name); err != nil {
		return nil, err
	}

	return &proto.UnregisterPluginResponse{}, nil
}

func (srv *GRPCServer) StopPlugin(ctx context.Context, req *proto.StopPluginRequest) (*proto.StopPluginResponse, error) {
	if err := srv.Impl.StopPlugin(ctx, req.Name); err != nil {
		return nil, err
	}

	return &proto.StopPluginResponse{}, nil
}

func (srv *GRPCServer) StartPlugin(ctx context.Context, req *proto.StartPluginRequest) (*proto.StartPluginResponse, error) {
	if err := srv.Impl.StartPlugin(ctx, req.Name); err != nil {
		return nil, err
	}

	return &proto.StartPluginResponse{}, nil
}

func (cli *GRPCClient) ListPlugins(ctx context.Context) ([]*proto.Plugin, error) {
	res, err := cli.Client.ListPlugins(ctx, &proto.ListPluginsRequest{})
	if err != nil {
		return nil, err
	}

	return res.Plugins, nil
}

func (cli *GRPCClient) RegisterPlugin(ctx context.Context, config *proto.PluginConfig) error {
	_, err := cli.Client.RegisterPlugin(ctx, &proto.RegisterPluginRequest{
		Config: config,
	})

	return err
}

func (cli *GRPCClient) UnregisterPlugin(ctx context.Context, name string) error {
	_, err := cli.Client.UnregisterPlugin(ctx, &proto.UnregisterPluginRequest{
		Name: name,
	})

	return err
}

func (cli *GRPCClient) StartPlugin(ctx context.Context, name string) error {
	_, err := cli.Client.StartPlugin(ctx, &proto.StartPluginRequest{
		Name: name,
	})

	return err
}

func (cli *GRPCClient) StopPlugin(ctx context.Context, name string) error {
	_, err := cli.Client.StopPlugin(ctx, &proto.StopPluginRequest{
		Name: name,
	})

	return err
}

var (
	_ proto.PluginManagerServiceServer = new(GRPCServer)
	_ Service                          = new(GRPCClient)
)
