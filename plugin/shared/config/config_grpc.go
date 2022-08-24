package config

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	GRPCClient struct {
		Client proto.ConfigServiceClient
	}

	GRPCServer struct {
		proto.UnimplementedConfigServiceServer

		Impl       Service
		PluginName string
	}
)

func (cli *GRPCClient) RegisterOption(ctx context.Context, option *proto.Option) error {
	_, err := cli.Client.RegisterOption(ctx, &proto.RegisterRequest{
		Option: option,
	})

	if err != nil {
		return err
	}
	return nil
}

func (cli *GRPCClient) GetValue(ctx context.Context, key string) (*proto.Value, error) {
	res, err := cli.Client.GetValue(ctx, &proto.GetValueRequest{
		Key: key,
	})

	if err != nil {
		return nil, err
	}

	return res.Value, nil
}

func (cli *GRPCClient) WatchValue(ctx context.Context, key ...string) (<-chan *proto.WatchChangesResponse, error) {
	res, err := cli.Client.WatchValues(ctx, &proto.WatchChangesRequest{
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

func (srv *GRPCServer) RegisterOption(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	err := srv.Impl.RegisterOption(ctx, req.Option)
	if err != nil {
		return nil, err
	}

	return &proto.RegisterResponse{}, nil
}

func (srv *GRPCServer) GetValue(ctx context.Context, req *proto.GetValueRequest) (*proto.GetValueResponse, error) {
	res, err := srv.Impl.GetValue(ctx, req.Key)
	if err != nil {
		return nil, err
	}

	return &proto.GetValueResponse{
		Value: res,
	}, nil
}

func (srv *GRPCServer) WatchValues(req *proto.WatchChangesRequest, stream proto.ConfigService_WatchValuesServer) error {
	ch, err := srv.Impl.WatchValue(stream.Context(), req.Keys...)
	if err != nil {
		return err
	}

	for msg := range ch {
		stream.Send(msg)
	}

	return nil
}

var _ proto.ConfigServiceServer = new(GRPCServer)
