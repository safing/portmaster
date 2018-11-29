package network

import (
	"github.com/Safing/portbase/modules"
)

func init() {
	modules.Register("network", nil, start, nil, "database")
}

func start() error {
	go cleaner()
	return registerAsDatabase()
}
