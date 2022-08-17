package shared

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"github.com/safing/portmaster/plugin/shared/proto"
	"google.golang.org/grpc"
)

// Handshake is a common handshake that is shared by plugin and host.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PORMASTER_PLUGIN",
	MagicCookieValue: "hello",
}

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]plugin.Plugin{
	"decider": &DeciderPlugin{},
}

type Decider interface {
	DecideOnConnection() error
}

type DeciderPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	// Concrete implementation, written in Go. This is only used for plugins
	// that are written in Go.
	Impl Decider
}

func (p *DeciderPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	proto.RegisterDeciderServiceServer(s, &GRPCServer{
		Impl:   p.Impl,
		broker: broker,
	})
	return nil
}

func (p *DeciderPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{
		client: proto.NewDeciderServiceClient(c),
		broker: broker,
	}, nil
}

var _ plugin.GRPCPlugin = &DeciderPlugin{}
