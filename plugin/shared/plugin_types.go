package shared

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
