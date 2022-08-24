package framework

import (
	"context"
	"encoding/json"

	"github.com/safing/portmaster/plugin/shared/base"
	"github.com/safing/portmaster/plugin/shared/config"
	"github.com/safing/portmaster/plugin/shared/notification"
	"github.com/safing/portmaster/plugin/shared/proto"
)

// BasePlugin implements base.Base and is used to
// provide plugins with easy access to the configuration
// provided when the plugin is first dispensed. It also provides
// access to the the Portmaster configuration and notification
// sytems.
type BasePlugin struct {
	Config       config.Service
	Notification notification.Service

	*proto.ConfigureRequest

	onInitFunc []func(ctx context.Context) error
}

// Configure is called by the plugin host (the Portmaster) and configures
// the plugin with static configuration and also provides access to the
// configuration and notification systems.
func (base *BasePlugin) Configure(ctx context.Context, env *proto.ConfigureRequest, configService config.Service, notifService notification.Service) error {
	base.ConfigureRequest = env
	base.Config = configService
	base.Notification = notifService

	for _, fn := range base.onInitFunc {
		if err := fn(ctx); err != nil {
			return err
		}
	}

	return nil
}

// BaseDirectory returns the installation directory of the Portmaster.
func (base *BasePlugin) BaseDirectory() string {
	return base.ConfigureRequest.BaseDirectory
}

// PluginName returns the name of the plugin as specified by the user.
func (base *BasePlugin) PluginName() string {
	return base.ConfigureRequest.PluginName
}

// ParseStaticConfig parses any static plugin configuration, specified in
// plugins.json into receiver.
//
// It returns ErrNoStaticConfig if the "config" field of the plugin configration
// was empty or unset.
// Otherwise it will return any error encountered during JSON unmarshaling.
func (base *BasePlugin) ParseStaticConfig(receiver interface{}) error {
	if len(base.StaticConfig) == 0 {
		return ErrNoStaticConfig
	}

	return json.Unmarshal(base.StaticConfig, receiver)
}

// OnInit registers a new on-init method that is executed when
// the plugin is dispensed and the Base.Configure() has been
// called by the Portmaster.
//
// Functions executed in this context are already save to access
// the configuration request, static configuration and BaseDirectory/PluginName.
func (base *BasePlugin) OnInit(fn func(context.Context) error) {
	base.onInitFunc = append(base.onInitFunc, fn)
}

var _ base.Base = new(BasePlugin)
