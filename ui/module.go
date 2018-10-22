package ui

import (
	"github.com/Safing/portbase/modules"
)

func init() {
	modules.Register("ui", prep, start, stop, "database", "api")
}

func prep() error {
	return nil
}

func stop() error {
	return nil
}
