package loader

import (
	"context"
	"fmt"

	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/pluginmanager"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	// HostPluginServer implements the PluginManagerService defined in shared/proto/plugin.proto
	// and allows privileged plugins to manage plugins registered and started at the Portmaster.
	// Note that plugins only have access to the PluginManagerService if the plugin configuration
	// marks the plugin as privileged.
	HostPluginServer struct {
		loader *PluginLoader
	}
)

// NewHostPluginServer returns a host plugin server that implements pluginmanager.Service by managing
// plugins loaded and configured in the PluginLoader impl.
func NewHostPluginServer(impl *PluginLoader) *HostPluginServer {
	return &HostPluginServer{
		loader: impl,
	}
}

func (srv *HostPluginServer) ListPlugins(ctx context.Context) ([]*proto.Plugin, error) {
	pluginConfigs := srv.loader.PluginConfigs()
	pluginInstances := srv.loader.PluginInstances()

	lm := make(map[string]*PluginInstance)

	for _, instance := range pluginInstances {
		lm[instance.Name] = instance
	}

	res := make([]*proto.Plugin, len(pluginConfigs))

	for idx, cfg := range pluginConfigs {
		instance, ok := lm[cfg.Name]

		protoTypes, err := pluginTypesToProto(cfg.Types)
		if err != nil {
			return nil, err
		}

		res[idx] = &proto.Plugin{
			Config: &proto.PluginConfig{
				Name:         cfg.Name,
				Privileged:   cfg.Privileged,
				StaticConfig: cfg.Config,
				PluginTypes:  protoTypes,
			},
		}

		if ok {
			res[idx].Instance = &proto.PluginInstance{
				Errors: instance.ReportedErrors(),
			}
		}
	}

	return res, nil
}

func (srv *HostPluginServer) RegisterPlugin(ctx context.Context, req *proto.PluginConfig) error {
	var pTypes []shared.PluginType

	for _, protoType := range req.PluginTypes {
		var pType shared.PluginType

		switch protoType {
		case proto.PluginType_PLUGIN_TYPE_BASE:
			pType = shared.PluginTypeBase
		case proto.PluginType_PLUGIN_TYPE_DECIDER:
			pType = shared.PluginTypeDecider
		case proto.PluginType_PLUGIN_TYPE_REPORTER:
			pType = shared.PluginTypeReporter
		case proto.PluginType_PLUGIN_TYPE_RESOLVER:
			pType = shared.PluginTypeResolver
		default:
			return fmt.Errorf("unsupported proto plugin type %s", protoType.String())
		}

		pTypes = append(pTypes, pType)
	}

	srv.loader.RegisterPlugin(shared.PluginConfig{
		Name:             req.Name,
		Types:            pTypes,
		Privileged:       req.Privileged,
		Config:           req.StaticConfig,
		DisableAutostart: req.DisableAutostart,
	})

	return nil
}

func (srv *HostPluginServer) UnregisterPlugin(ctx context.Context, name string) error {
	return srv.loader.UnregisterPlugin(ctx, name)
}

func (srv *HostPluginServer) StartPlugin(ctx context.Context, name string) error {
	_, err := srv.loader.Dispense(ctx, name)
	return err
}

func (srv *HostPluginServer) StopPlugin(ctx context.Context, name string) error {
	return srv.loader.StopInstance(ctx, name)
}

func pluginTypesToProto(v []shared.PluginType) ([]proto.PluginType, error) {
	protoTypes := make([]proto.PluginType, len(v))
	for _, pType := range v {
		var protoType proto.PluginType
		switch pType {
		case "base":
			protoType = proto.PluginType_PLUGIN_TYPE_BASE
		case "decider":
			protoType = proto.PluginType_PLUGIN_TYPE_DECIDER
		case "reporter":
			protoType = proto.PluginType_PLUGIN_TYPE_REPORTER
		case "resolver":
			protoType = proto.PluginType_PLUGIN_TYPE_RESOLVER
		default:
			return nil, fmt.Errorf("unsupported plugin type: %s", pType)
		}

		protoTypes = append(protoTypes, protoType)
	}

	return protoTypes, nil
}

var _ pluginmanager.Service = new(HostPluginServer)
