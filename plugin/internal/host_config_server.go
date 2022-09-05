package internal

import (
	"context"
	"fmt"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/plugin/shared/proto"
)

// HostConfigServer is used by GRPCServer to provide plugins with access to the
// Portmaster configuration system. It's created on a per-plugin basis by the loader.PluginLoader
// when Dispense'ing a new plugin instance.
type HostConfigServer struct {
	fanout     *EventFanout
	pluginName string
	privileged bool
}

// NewHostConfigServer creates a new HostConfigServer that provides scoped access to
// the Portmaster configuration system for a plugin named pluginName.
// Access and creation of configuration options is limited to the "config:plugins/<pluginName>" scope
// while keys are transparently proxied for unprivileged plugins.
//
// Note that while privileged plugins may access configuration options outside the "plugins/<pluginName>"
// scope, option keys created by a plugin are always prefixed with the scope to allow grouping of
// those options in the user interface of the Portmaster.
func NewHostConfigServer(fanout *EventFanout, pluginName string, privileged bool) *HostConfigServer {
	return &HostConfigServer{
		pluginName: pluginName,
		privileged: privileged,
		fanout:     fanout,
	}
}

func (cfg *HostConfigServer) RegisterOption(ctx context.Context, req *proto.Option) error {
	defaultValue, err := UnwrapConfigValue(req.Default, OptionTypeToConfig(req.OptionType))
	if err != nil {
		return err
	}

	key := fmt.Sprintf("plugins/%s/%s", cfg.pluginName, req.Key)

	err = config.Register(&config.Option{
		Name:           req.Name,
		Help:           req.Help,
		Description:    req.Description,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		OptType:        OptionTypeToConfig(req.OptionType),
		Key:            key,
		DefaultValue:   defaultValue,
		Annotations: config.Annotations{
			config.CategoryAnnotation:  cfg.pluginName,
			config.SubsystemAnnotation: "plugins",
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func (cfg *HostConfigServer) GetValue(ctx context.Context, key string) (*proto.Value, error) {
	if !cfg.privileged {
		key = fmt.Sprintf("plugins/%s/%s", cfg.pluginName, key)
	}

	return GetConfigValueProto(key)
}

func (cfg *HostConfigServer) WatchValue(ctx context.Context, keys ...string) (<-chan *proto.WatchChangesResponse, error) {
	if !cfg.privileged {
		for idx, key := range keys {
			keys[idx] = fmt.Sprintf("plugins/%s/%s", cfg.pluginName, key)
		}
	}

	ch := cfg.fanout.SubscribeConfigChanges(ctx, cfg.pluginName, keys)

	return ch, nil
}
