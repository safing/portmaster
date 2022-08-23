package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type Decider interface {
	DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error)
}

type DeciderPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	// Concrete implementation, written in Go. This is only used for plugins
	// that are written in Go.
	Impl Decider
}

func (p *DeciderPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterDeciderServiceServer(s, &GRPCDeciderServer{
		Impl:   p.Impl,
		broker: broker,
	})

	return nil
}

func (p *DeciderPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCDeciderClient{
		client: proto.NewDeciderServiceClient(c),
		broker: broker,
	}, nil
}

var _ plugin.GRPCPlugin = &DeciderPlugin{}
