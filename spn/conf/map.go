package conf

import (
	"flag"

	"github.com/safing/portmaster/spn/hub"
)

// Primary Map Configuration.
var (
	MainMapName  = "main"
	MainMapScope = hub.ScopePublic
)

func init() {
	flag.StringVar(&MainMapName, "spn-map", "main", "set main SPN map - use only for testing")
}
