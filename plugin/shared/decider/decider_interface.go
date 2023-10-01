package decider

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

type (
	Decider interface {
		DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error)
	}

	Plugin struct {
		plugin.NetRPCUnsupportedPlugin
		// Concrete implementation, written in Go. This is only used for plugins
		// that are written in Go.
		Impl Decider
	}
)

// GRPCServer configures a gRPC Decider Service server on s and implements plugin.GRPCPlugin.
func (p *Plugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterDeciderServiceServer(s, &gRPCServer{
		Impl:   p.Impl,
		broker: broker,
	})

	return nil
}

// GRPCClient returns a Decider implementation that talks to a gRPC server and implements plugin.GRPCPlugin.
func (p *Plugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &gRPCClient{
		client: proto.NewDeciderServiceClient(c),
		broker: broker,
	}, nil
}

var _ plugin.GRPCPlugin = &Plugin{}
