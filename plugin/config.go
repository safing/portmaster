package plugin

import (
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
}
