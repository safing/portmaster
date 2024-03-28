package intel

import (
	"github.com/safing/portbase/modules"
	_ "github.com/safing/portmaster/service/intel/customlists"
)

// Module of this package. Export needed for testing of the endpoints package.
var Module *modules.Module

func init() {
	Module = modules.Register("intel", nil, nil, nil, "geoip", "filterlists", "customlists")
}
