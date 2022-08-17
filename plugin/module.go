package plugin

import "github.com/safing/portbase/modules"

type Module struct {
	*modules.Module
}

func init() {
	mod := new(Module)
	m := modules.Register("plugin", mod.prepare, mod.start, mod.stop, "core", "network")

	mod.Module = m
}

func (m *Module) prepare() error {
	return nil
}

func (m *Module) start() error {
	return nil
}

func (m *Module) stop() error {
	return nil
}
