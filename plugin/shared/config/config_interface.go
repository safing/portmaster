package config

import (
	"context"

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
)
