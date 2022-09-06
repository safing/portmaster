package resolver

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	gRPCClient struct {
		broker *plugin.GRPCBroker
		client proto.ResolverServiceClient
	}

	gRPCServer struct {
		proto.UnimplementedResolverServiceServer

		Impl Resolver
	}
)

func (cli *gRPCClient) Resolve(ctx context.Context, req *proto.DNSQuestion, conn *proto.Connection) (*proto.DNSResponse, error) {
	res, err := cli.client.Resolve(ctx, &proto.ResolveRequest{
		Question:   req,
		Connection: conn,
	})
	if err != nil {
		return nil, err
	}

	return res.GetResponse(), nil
}

func (srv *gRPCServer) Resolve(ctx context.Context, req *proto.ResolveRequest) (*proto.ResolveResponse, error) {
	res, err := srv.Impl.Resolve(ctx, req.GetQuestion(), req.GetConnection())
	if err != nil {
		return nil, err
	}

	return &proto.ResolveResponse{
		Response: res,
	}, nil
}
