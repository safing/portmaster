package framework

import (
	"context"

	"github.com/safing/portmaster/plugin/shared"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type basePlugin struct {
	shared.Config

	*proto.ConfigureRequest

	onInitFunc []func() error
}

func (base *basePlugin) Configure(ctx context.Context, env *proto.ConfigureRequest, configService shared.Config) error {
	base.ConfigureRequest = env
	base.Config = configService

	for _, fn := range base.onInitFunc {
		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}

func (base *basePlugin) BaseDirectory() string {
	return base.ConfigureRequest.BaseDirectory
}

func (base *basePlugin) PluginName() string {
	return base.ConfigureRequest.PluginName
}

func (base *basePlugin) OnInit(fn func() error) {
	base.onInitFunc = append(base.onInitFunc, fn)
}

var _ shared.Base = new(basePlugin)
