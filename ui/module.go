package ui

import (
	"github.com/safing/portbase/modules"
)

func init() {
	modules.Register("ui", prep, nil, nil, "updates", "api")
}

func prep() error {
	return registerRoutes()
}
