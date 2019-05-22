package ui

import (
	"github.com/Safing/portbase/api"
	"github.com/Safing/portbase/modules"
)

func init() {
	modules.Register("ui", prep, nil, nil, "updates", "api")
}

func prep() error {
	api.SetDefaultAPIListenAddress("127.0.0.1:817")
	return registerRoutes()
}
