package plugin

import (
	"encoding/json"

	"github.com/safing/portmaster/plugin/shared"
)

var (
	CfgKeyEnablePlugins = "plugins/enablePlugins"
)

type PluginConfig struct {
	// Name is the name of the plugin that should be launched.
	Name string `json:"name"`

	// Types holds a list of plugin-types that should be used.
	// Note that the plugin must implement the types listed in this
	// field.
	// All plugins must implement the "base" plugin type so listing
	// it is not required.
	Types []shared.PluginType `json:"types"`

	// Config may hold static configuration of the plugin.
	// Please refer to the documentation of the plugins for
	// more information if static configuration is supported/required
	// and how the configuration should be structured.
	//
	// The value of this is passed as raw-bytes the the plugin when
	// it is first dispensed by the plugin loader.
	Config json.RawMessage `json:"config,omitempty"`
}
