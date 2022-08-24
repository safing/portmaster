package config

import (
	"context"
	"fmt"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	// Service is the interface that allows scoped interaction with the
	// Portmaster configuration system.
	// It is passed to plugins using Base.Configure() and provided by the
	// loader.PluginLoader when a plugin is first dispensed and initialized.
	//
	// Plugins may use the Service to register new configuration options that
	// the user can specify and configure using the Portmaster User Interface.
	Service interface {
		// RegisterOption registers a new configuration option in the Portmaster
		// configuration system. Once registered, a user may alter the configuration
		// using the Portmaster User Interface.
		//
		// Please refer to the documentation of proto.Option for more information
		// about required fields and how configuration options are handled.
		RegisterOption(ctx context.Context, option *proto.Option) error

		// GetValue returns the current value of a Portmaster configuration option
		// identified by it's key.
		//
		// Note that plugins only have access to keys the registered. (Plugin keys are scoped
		// by plugin-name.)
		GetValue(ctx context.Context, key string) (*proto.Value, error)
		WatchValue(ctx context.Context, key ...string) (<-chan *proto.WatchChangesResponse, error)
	}

	// HostConfigServer is used by GRPCServer to provide plugins with access to the
	// Portmaster configuration system. It's created on a per-plugin basis by the loader.PluginLoader
	// when Dispense'ing a new plugin instance.
	HostConfigServer struct {
		fanout     *shared.EventFanout
		pluginName string
	}
)

// NewHostConfigServer creates a new HostConfigServer that provides scoped access to
// the Portmaster configuration system for a plugin named pluginName.
// Access and creation of configuration options is limited to the "config:plugins/<pluginName>" scope
// while keys are transparently proxied for the plugin.
func NewHostConfigServer(fanout *shared.EventFanout, pluginName string) *HostConfigServer {
	return &HostConfigServer{
		pluginName: pluginName,
	}
}

func (cfg *HostConfigServer) RegisterOption(ctx context.Context, req *proto.Option) error {
	defaultValue, err := proto.UnwrapConfigValue(req.Default, proto.OptionTypeToConfig(req.OptionType))
	if err != nil {
		return err
	}

	err = config.Register(&config.Option{
		Name:           req.Name,
		Help:           req.Help,
		Description:    req.Description,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		OptType:        proto.OptionTypeToConfig(req.OptionType),
		Key:            fmt.Sprintf("plugins/%s/%s", cfg.pluginName, req.Key),
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
	key = fmt.Sprintf("plugins/%s/%s", cfg.pluginName, key)

	return shared.GetConfigValueProto(key)
}

func (cfg *HostConfigServer) WatchValue(ctx context.Context, keys ...string) (<-chan *proto.WatchChangesResponse, error) {
	ch := cfg.fanout.SubscribeConfigChanges(ctx, cfg.pluginName, keys)
	return ch, nil
}
