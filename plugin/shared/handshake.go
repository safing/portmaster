package shared

import "github.com/hashicorp/go-plugin"

// Handshake is a common handshake that is shared by plugin and host.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "PORTMASTER_PLUGIN",
	MagicCookieValue: "hello",
}

var PluginMap = plugin.PluginSet{
	"base":     &BasePlugin{},
	"decider":  &DeciderPlugin{},
	"reporter": &ReporterPlugin{},
}
