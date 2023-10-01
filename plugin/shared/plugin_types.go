package shared

import "encoding/json"

type PluginType string

const (
	PluginTypeBase     = "base"
	PluginTypeDecider  = "decider"
	PluginTypeResolver = "resolver"
	PluginTypeReporter = "reporter"
)

// IsValidPluginType returns true if pt is a valid
// and known plugin type.
func IsValidPluginType(pt PluginType) bool {
	switch pt {
	case PluginTypeBase, PluginTypeDecider, PluginTypeReporter, PluginTypeResolver:
		return true
	default:
		return false
	}
}

type PluginConfig struct {
	// Name is the name of the plugin that should be launched.
	Name string `json:"name"`

	// Types holds a list of plugin-types that should be used.
	// Note that the plugin must implement the types listed in this
	// field.
	// All plugins must implement the "base" plugin type so listing
	// it is not required.
	Types []PluginType `json:"types,omitempty"`

	// Privileged may be set to true to grant the plugin additional
	// capabilities like reading/modifying all configuration values
	// or to manage the plugin system.
	Privileged bool `json:"privileged,omitempty"`

	// DisableAutostart defines whether or not the plugin should be started
	// when Portmaster starts
	DisableAutostart bool `json:"disableAutostart,omitempty"`

	// Config may hold static configuration of the plugin.
	// Please refer to the documentation of the plugins for
	// more information if static configuration is supported/required
	// and how the configuration should be structured.
	//
	// The value of this is passed as raw-bytes the the plugin when
	// it is first dispensed by the plugin loader.
	Config json.RawMessage `json:"config,omitempty"`
}
