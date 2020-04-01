package intel

import (
	"github.com/safing/portbase/modules"
)

func init() {
	modules.Register("intel", nil, nil, nil, "geoip")
}
