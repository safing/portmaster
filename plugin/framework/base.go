package framework

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

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

	baseCtx context.Context
	cancel  context.CancelFunc

	onInitFunc     []func(ctx context.Context) error
	onShutdownFunc []func(ctx context.Context) error
}

// Configure is called by the plugin host (the Portmaster) and configures
// the plugin with static configuration and also provides access to the
// configuration and notification systems.
func (base *BasePlugin) Configure(ctx context.Context, env *proto.ConfigureRequest, configService config.Service, notifService notification.Service) error {
	log.Println("received configuration request")

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

func (base *BasePlugin) Shutdown(ctx context.Context) error {
	log.Println("received shutdown request")

	for _, fn := range base.onShutdownFunc {
		if err := fn(ctx); err != nil {
			return err
		}
	}

	base.cancel()

	go func() {
		time.Sleep(time.Second)
		os.Exit(0)
	}()

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

// Context returns the context.Context of the plugin. The returned context
// is cancelled as soon as the plugin is requested to stop.
func (base *BasePlugin) Context() context.Context {
	return base.baseCtx
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

// OnShutdown registers a new on-shutdown method that is executed
// when the plugin shutdown is requested by the plugin host (Portmaster).
func (base *BasePlugin) OnShutdown(fn func(context.Context) error) {
	base.onShutdownFunc = append(base.onShutdownFunc, fn)
}

var _ base.Base = new(BasePlugin)
