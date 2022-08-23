package shared

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	GRPCConfigClient struct {
		client proto.ConfigServiceClient
	}

	GRPCConfigServer struct {
		proto.UnimplementedConfigServiceServer

		Impl       Config
		PluginName string
	}
)

func (cli *GRPCConfigClient) RegisterOption(ctx context.Context, option *proto.Option) error {
	_, err := cli.client.RegisterOption(ctx, &proto.RegisterRequest{
		Option: option,
	})

	if err != nil {
		return err
	}
	return nil
}

func (cli *GRPCConfigClient) GetValue(ctx context.Context, key string) (*proto.Value, error) {
	res, err := cli.client.GetValue(ctx, &proto.GetValueRequest{
		Key: key,
	})

	if err != nil {
		return nil, err
	}

	return res.Value, nil
}

func (cli *GRPCConfigClient) WatchValue(ctx context.Context, key ...string) (<-chan *proto.WatchChangesResponse, error) {
	res, err := cli.client.WatchValues(ctx, &proto.WatchChangesRequest{
		Keys: key,
	})

	if err != nil {
		return nil, err
	}

	ch := make(chan *proto.WatchChangesResponse)

	go func() {
		defer close(ch)

		for {
			msg, err := res.Recv()

			if err != nil {
				// TODO(ppacher): should we notify the caller about the error
				return
			}
			ch <- msg
		}
	}()

	return ch, nil
}

func (srv *GRPCConfigServer) RegisterOption(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	err := srv.Impl.RegisterOption(ctx, req.Option)
	if err != nil {
		return nil, err
	}

	return &proto.RegisterResponse{}, nil
}

func (srv *GRPCConfigServer) GetValue(ctx context.Context, req *proto.GetValueRequest) (*proto.GetValueResponse, error) {
	res, err := srv.Impl.GetValue(ctx, req.Key)
	if err != nil {
		return nil, err
	}

	return &proto.GetValueResponse{
		Value: res,
	}, nil
}

func (srv *GRPCConfigServer) WatchValues(req *proto.WatchChangesRequest, stream proto.ConfigService_WatchValuesServer) error {
	return nil
}

var _ proto.ConfigServiceServer = new(GRPCConfigServer)
