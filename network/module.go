package network

import (
	"github.com/Safing/portbase/modules"
)

func init() {
	modules.Register("network", prep, start, nil, "database")
}

func start() error {
	return registerAsDatabase()
}
