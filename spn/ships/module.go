package ships

import (
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/spn/conf"
)

var module *modules.Module

func init() {
	module = modules.Register("ships", start, nil, nil, "cabin")
}

func start() error {
	if conf.PublicHub() {
		initPageInput()
	}

	return nil
}
