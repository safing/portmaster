package network

import (
	"github.com/safing/portbase/modules"
)

func init() {
	modules.Register("network", nil, start, nil, "database")
}

func start() error {
	go cleaner()
	return registerAsDatabase()
}
