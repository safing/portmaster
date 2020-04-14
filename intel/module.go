package intel

import (
	"github.com/safing/portbase/modules"
)

var (
	// Module of this package. Export needed for testing of the endpoints package.
	Module *modules.Module
)

func init() {
	Module = modules.Register("intel", nil, nil, nil, "geoip", "filterlists")
}
