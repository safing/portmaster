package shared

type PluginType string

const (
	PluginTypeBase     = "base"
	PluginTypeDecider  = "decider"
	PluginTypeResolver = "resolver"
	PluginTypeReporter = "reporter"
)
