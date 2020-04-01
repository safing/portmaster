package process

import (
	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module
)

func init() {
	module = modules.Register("processes", prep, nil, nil, "profiles")
}

func prep() error {
	return registerConfiguration()
}
