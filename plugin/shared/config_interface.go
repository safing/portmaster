package shared

import (
	"context"
	"fmt"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	// Config is the interface that allows scoped interaction with the
	// Portmaster configuration system.
	// It is passed to plugins using Base.Configure() and provided by the
	// loader.PluginLoader when a plugin is first dispensed and initialized.
	//
	// Plugins may use the Config to register new configuration options that
	// the user can specify and configure using the Portmaster User Interface.
	Config interface {
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

	// HostConfigServer is used by GRPCConfigServer to provide plugins with access to the
	// Portmaster configuration system. It's created on a per-plugin basis by the loader.PluginLoader
	// when Dispense'ing a new plugin instance.
	HostConfigServer struct {
		module     *modules.Module
		pluginName string
	}
)

// NewHostConfigServer creates a new HostConfigServer that provides scoped access to
// the Portmaster configuration system for a plugin named pluginName.
// Access and creation of configuration options is limited to the "config:plugins/<pluginName>" scope
// while keys are transparently proxied for the plugin.
func NewHostConfigServer(module *modules.Module, pluginName string) *HostConfigServer {
	return &HostConfigServer{
		module:     module,
		pluginName: pluginName,
	}
}

func (cfg *HostConfigServer) RegisterOption(ctx context.Context, req *proto.Option) error {
	defaultValue, err := proto.UnwrapValue(req.Default, proto.OptionTypeToConfig(req.OptionType))
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
	opt, err := config.GetOption(fmt.Sprintf("plugins/%s/%s", cfg.pluginName, key))
	if err != nil {
		return nil, err
	}

	var value *proto.Value

	switch opt.OptType {
	case config.OptTypeBool:
		value = &proto.Value{
			Bool: config.Concurrent.GetAsBool(opt.Key, false)(),
		}

	case config.OptTypeInt:
		value = &proto.Value{
			Int: config.Concurrent.GetAsInt(opt.Key, 0)(),
		}

	case config.OptTypeString:
		value = &proto.Value{
			String_: config.Concurrent.GetAsString(opt.Key, "")(),
		}

	case config.OptTypeStringArray:
		value = &proto.Value{
			StringArray: config.Concurrent.GetAsStringArray(opt.Key, []string{})(),
		}

	default:
		return nil, fmt.Errorf("unsupported option type %d", opt.OptType)
	}

	return value, nil
}

func (cfg *HostConfigServer) WatchValue(ctx context.Context, key ...string) (<-chan *proto.WatchChangesResponse, error) {
	return nil, nil
}
